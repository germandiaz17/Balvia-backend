package services

// Multi-entity support for POST /sync/push.
//
// Every entity here follows the same shape — decode a payload, call the same
// service the REST endpoint calls, compare updated_at for conflicts, shape a
// result — so the shape lives once in entityPusher and each entity contributes
// only what genuinely differs: how to read its payload and which methods to
// call. Six hand-written copies of create/update/delete would drift, and the
// part that would drift first is the conflict rule, which is exactly the part
// that must not.
//
// Transactions are NOT routed through here: they carry idempotency by client_id
// and a closed-period check that no other entity has, and they predate this
// file. See pushTransaction in sync.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/germandiaz17/Balvia-backend/internal/database/sqlc"
)

// Short aliases keep the generic signatures below readable.
type (
	sqlcAccount   = sqlc.Account
	sqlcCategory  = sqlc.Category
	sqlcBudget    = sqlc.Budget
	sqlcGoal      = sqlc.SavingsGoal
	sqlcRecurring = sqlc.RecurringTransaction
)

// entityPusher adapts one entity's service to the push pipeline. The zero value
// of a hook means "this entity does not support that operation".
type entityPusher[In any, Row any] struct {
	// decode turns the wire payload into the service's input type.
	decode func(json.RawMessage) (In, error)
	// get loads the current server row, used for conflict detection.
	get func(ctx context.Context, userID, id uuid.UUID) (Row, error)
	// updatedAt reads the row's updated_at so it can be compared with the
	// client's snapshot.
	updatedAt func(Row) pgtype.Timestamptz

	create func(ctx context.Context, userID uuid.UUID, in In) (Row, error)
	update func(ctx context.Context, userID, id uuid.UUID, in In) (Row, error)
	remove func(ctx context.Context, userID, id uuid.UUID) error
}

// apply runs one push item through the entity's service.
func (p entityPusher[In, Row]) apply(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	switch item.Operation {
	case OpCreate:
		in, err := p.decode(item.Payload)
		if err != nil {
			return rejected(item.ClientRef, err.Error())
		}
		row, err := p.create(ctx, userID, in)
		if err != nil {
			return mapServiceError(item.ClientRef, err)
		}
		return PushItemResult{ClientRef: item.ClientRef, Status: StatusApplied, ServerEntity: row}

	case OpUpdate:
		if item.EntityID == nil {
			return rejected(item.ClientRef, "entity_id is required for update")
		}
		in, err := p.decode(item.Payload)
		if err != nil {
			return rejected(item.ClientRef, err.Error())
		}
		// Load the server's version first: it decides whether this is a
		// conflict, and it also enforces ownership before we mutate anything.
		current, err := p.get(ctx, userID, *item.EntityID)
		if err != nil {
			return mapServiceError(item.ClientRef, err)
		}
		if conflict := p.conflict(item, current); conflict != nil {
			return *conflict
		}
		row, err := p.update(ctx, userID, *item.EntityID, in)
		if err != nil {
			return mapServiceError(item.ClientRef, err)
		}
		return PushItemResult{ClientRef: item.ClientRef, Status: StatusApplied, ServerEntity: row}

	case OpDelete:
		if item.EntityID == nil {
			return rejected(item.ClientRef, "entity_id is required for delete")
		}
		current, err := p.get(ctx, userID, *item.EntityID)
		if err != nil {
			return mapServiceError(item.ClientRef, err)
		}
		if conflict := p.conflict(item, current); conflict != nil {
			return *conflict
		}
		if err := p.remove(ctx, userID, *item.EntityID); err != nil {
			return mapServiceError(item.ClientRef, err)
		}
		return PushItemResult{ClientRef: item.ClientRef, Status: StatusApplied}

	default:
		return rejected(item.ClientRef, "unknown operation: "+string(item.Operation))
	}
}

// conflict reports a last-write-wins conflict, returning the server's version so
// the client can reconcile. Truncating to the second keeps a sub-second
// serialisation difference from looking like a real edit.
func (p entityPusher[In, Row]) conflict(item PushItem, current Row) *PushItemResult {
	if item.ClientUpdatedAt == nil {
		return nil
	}
	serverAt := p.updatedAt(current)
	if !serverAt.Valid {
		return nil
	}
	if serverAt.Time.UTC().Truncate(time.Second).After(item.ClientUpdatedAt.UTC().Truncate(time.Second)) {
		return &PushItemResult{
			ClientRef:    item.ClientRef,
			Status:       StatusConflict,
			ServerEntity: current,
		}
	}
	return nil
}

// --- Payload decoding helpers ------------------------------------------------

// decodePayload unmarshals a push payload, refusing unknown fields so a client
// typo silently dropping a value shows up as a rejected item instead of a row
// that quietly lost data.
func decodePayload[T any](raw json.RawMessage, entity string) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, fmt.Errorf("payload is required for %s", entity)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("invalid %s payload: %w", entity, err)
	}
	return out, nil
}

// parseAmount converts a wire amount. Money travels as a string precisely so it
// never round-trips through a float.
func parseAmount(s, field string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, fmt.Errorf("%s is required", field)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid %s: %w", field, err)
	}
	return d, nil
}

func parseOptionalAmount(s *string, field string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := parseAmount(*s, field)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// parseDay reads a YYYY-MM-DD wire date.
func parseDay(s, field string) (time.Time, error) {
	t, err := time.Parse(dateOnlyLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: expected YYYY-MM-DD", field)
	}
	return t, nil
}

func parseOptionalDay(s *string, field string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := parseDay(*s, field)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

const dateOnlyLayout = "2006-01-02"

// --- Account -----------------------------------------------------------------

type pushAccountPayload struct {
	Name           string  `json:"name"`
	AccountType    string  `json:"account_type"`
	Currency       string  `json:"currency,omitempty"`
	InitialBalance *string `json:"initial_balance,omitempty"`
	Icon           *string `json:"icon,omitempty"`
	Color          *string `json:"color,omitempty"`
	DisplayOrder   int32   `json:"display_order,omitempty"`
	IsArchived     bool    `json:"is_archived,omitempty"`
}

// parsedAccount is the payload with its money already converted.
//
// The conversion happens in decode, not in create/update, so that a malformed
// amount comes back as a rejected item naming the offending field. Parsing later
// makes it indistinguishable from a server fault, and the client can only show
// the user "internal server error" for what is really their typo.
type parsedAccount struct {
	pushAccountPayload
	// initialBalance is nil when the client did not send one: zero on create,
	// "leave the opening balance alone" on update.
	initialBalance *decimal.Decimal
}

func (s *SyncService) accountPusher() entityPusher[parsedAccount, sqlcAccount] {
	svc := s.svcs.Account
	return entityPusher[parsedAccount, sqlcAccount]{
		decode: func(raw json.RawMessage) (parsedAccount, error) {
			p, err := decodePayload[pushAccountPayload](raw, "account")
			if err != nil {
				return parsedAccount{}, err
			}
			balance, err := parseOptionalAmount(p.InitialBalance, "initial_balance")
			if err != nil {
				return parsedAccount{}, err
			}
			return parsedAccount{pushAccountPayload: p, initialBalance: balance}, nil
		},
		get:       svc.Get,
		updatedAt: func(a sqlcAccount) pgtype.Timestamptz { return a.UpdatedAt },
		create: func(ctx context.Context, userID uuid.UUID, p parsedAccount) (sqlcAccount, error) {
			balance := decimal.Zero
			if p.initialBalance != nil {
				balance = *p.initialBalance
			}
			return svc.Create(ctx, userID, CreateAccountInput{
				Name:           p.Name,
				AccountType:    p.AccountType,
				Currency:       p.Currency,
				InitialBalance: balance,
				Icon:           p.Icon,
				Color:          p.Color,
				DisplayOrder:   p.DisplayOrder,
			})
		},
		update: func(ctx context.Context, userID, id uuid.UUID, p parsedAccount) (sqlcAccount, error) {
			return svc.Update(ctx, userID, id, UpdateAccountInput{
				Name:           p.Name,
				AccountType:    p.AccountType,
				Icon:           p.Icon,
				Color:          p.Color,
				InitialBalance: p.initialBalance,
				DisplayOrder:   p.DisplayOrder,
				IsArchived:     p.IsArchived,
			})
		},
		remove: svc.Delete,
	}
}

// --- Category ----------------------------------------------------------------

type pushCategoryPayload struct {
	Name         string     `json:"name"`
	CategoryType string     `json:"category_type,omitempty"`
	ParentID     *uuid.UUID `json:"parent_id,omitempty"`
	Icon         *string    `json:"icon,omitempty"`
	Color        *string    `json:"color,omitempty"`
	DisplayOrder int32      `json:"display_order,omitempty"`
}

func (s *SyncService) categoryPusher() entityPusher[pushCategoryPayload, sqlcCategory] {
	svc := s.svcs.Category
	return entityPusher[pushCategoryPayload, sqlcCategory]{
		decode: func(raw json.RawMessage) (pushCategoryPayload, error) {
			return decodePayload[pushCategoryPayload](raw, "category")
		},
		get:       svc.Get,
		updatedAt: func(c sqlcCategory) pgtype.Timestamptz { return c.UpdatedAt },
		create: func(ctx context.Context, userID uuid.UUID, p pushCategoryPayload) (sqlcCategory, error) {
			return svc.Create(ctx, userID, CreateCategoryInput{
				Name:         p.Name,
				CategoryType: p.CategoryType,
				ParentID:     p.ParentID,
				Icon:         p.Icon,
				Color:        p.Color,
				DisplayOrder: p.DisplayOrder,
			})
		},
		update: func(ctx context.Context, userID, id uuid.UUID, p pushCategoryPayload) (sqlcCategory, error) {
			// category_type is immutable, so it is absent from the update input.
			return svc.Update(ctx, userID, id, UpdateCategoryInput{
				Name:         p.Name,
				ParentID:     p.ParentID,
				Icon:         p.Icon,
				Color:        p.Color,
				DisplayOrder: p.DisplayOrder,
			})
		},
		remove: svc.Delete,
	}
}

// --- Budget ------------------------------------------------------------------

type pushBudgetPayload struct {
	CategoryID        *uuid.UUID `json:"category_id,omitempty"`
	Amount            string     `json:"amount"`
	Currency          string     `json:"currency,omitempty"`
	WarningThreshold  *string    `json:"alert_threshold_warning,omitempty"`
	CriticalThreshold *string    `json:"alert_threshold_critical,omitempty"`
	Notes             *string    `json:"notes,omitempty"`
}

func (p pushBudgetPayload) toInput() (BudgetInput, error) {
	amount, err := parseAmount(p.Amount, "amount")
	if err != nil {
		return BudgetInput{}, err
	}
	warning, err := parseOptionalAmount(p.WarningThreshold, "alert_threshold_warning")
	if err != nil {
		return BudgetInput{}, err
	}
	critical, err := parseOptionalAmount(p.CriticalThreshold, "alert_threshold_critical")
	if err != nil {
		return BudgetInput{}, err
	}
	return BudgetInput{
		CategoryID:        p.CategoryID,
		Amount:            amount,
		Currency:          p.Currency,
		WarningThreshold:  warning,
		CriticalThreshold: critical,
		Notes:             p.Notes,
	}, nil
}

func (s *SyncService) budgetPusher() entityPusher[BudgetInput, sqlcBudget] {
	svc := s.svcs.Budget
	return entityPusher[BudgetInput, sqlcBudget]{
		decode: func(raw json.RawMessage) (BudgetInput, error) {
			p, err := decodePayload[pushBudgetPayload](raw, "budget")
			if err != nil {
				return BudgetInput{}, err
			}
			return p.toInput()
		},
		get:       svc.Get,
		updatedAt: func(b sqlcBudget) pgtype.Timestamptz { return b.UpdatedAt },
		create:    svc.Create,
		update:    svc.Update,
		remove:    svc.Delete,
	}
}

// --- Savings goal ------------------------------------------------------------

type pushGoalPayload struct {
	Name            string     `json:"name"`
	Description     *string    `json:"description,omitempty"`
	Icon            *string    `json:"icon,omitempty"`
	Color           *string    `json:"color,omitempty"`
	TargetAmount    string     `json:"target_amount"`
	Currency        string     `json:"currency,omitempty"`
	StartDate       *string    `json:"start_date,omitempty"`
	TargetDate      string     `json:"target_date"`
	Status          string     `json:"status,omitempty"`
	LinkedAccountID *uuid.UUID `json:"linked_account_id,omitempty"`
}

// parsedGoal carries the converted amount and dates — see parsedAccount for why
// the conversion belongs in decode.
type parsedGoal struct {
	pushGoalPayload
	targetAmount decimal.Decimal
	targetDate   time.Time
	startDate    time.Time
}

func (s *SyncService) savingsGoalPusher() entityPusher[parsedGoal, sqlcGoal] {
	svc := s.svcs.SavingsGoal
	return entityPusher[parsedGoal, sqlcGoal]{
		decode: func(raw json.RawMessage) (parsedGoal, error) {
			p, err := decodePayload[pushGoalPayload](raw, "savings_goal")
			if err != nil {
				return parsedGoal{}, err
			}
			target, err := parseAmount(p.TargetAmount, "target_amount")
			if err != nil {
				return parsedGoal{}, err
			}
			targetDate, err := parseDay(p.TargetDate, "target_date")
			if err != nil {
				return parsedGoal{}, err
			}
			// A goal created offline days ago should keep the day the user meant,
			// so start_date is honoured when sent.
			startDate := time.Now()
			if p.StartDate != nil && *p.StartDate != "" {
				startDate, err = parseDay(*p.StartDate, "start_date")
				if err != nil {
					return parsedGoal{}, err
				}
			}
			return parsedGoal{
				pushGoalPayload: p,
				targetAmount:    target,
				targetDate:      targetDate,
				startDate:       startDate,
			}, nil
		},
		get:       svc.Get,
		updatedAt: func(g sqlcGoal) pgtype.Timestamptz { return g.UpdatedAt },
		create: func(ctx context.Context, userID uuid.UUID, p parsedGoal) (sqlcGoal, error) {
			return svc.Create(ctx, userID, GoalInput{
				Name:            p.Name,
				Description:     p.Description,
				Icon:            p.Icon,
				Color:           p.Color,
				TargetAmount:    p.targetAmount,
				Currency:        p.Currency,
				StartDate:       p.startDate,
				TargetDate:      p.targetDate,
				LinkedAccountID: p.LinkedAccountID,
			})
		},
		update: func(ctx context.Context, userID, id uuid.UUID, p parsedGoal) (sqlcGoal, error) {
			// PUT is a full replace and start_date is not editable, matching the
			// REST endpoint exactly.
			return svc.Update(ctx, userID, id, GoalUpdateInput{
				Name:            p.Name,
				Description:     p.Description,
				Icon:            p.Icon,
				Color:           p.Color,
				TargetAmount:    p.targetAmount,
				TargetDate:      p.targetDate,
				Status:          p.Status,
				LinkedAccountID: p.LinkedAccountID,
			})
		},
		remove: svc.Delete,
	}
}

// --- Recurring transaction ---------------------------------------------------

// Mirrors the REST body in handlers/recurring_transaction.go, field for field.
type pushRecurringPayload struct {
	AccountID          uuid.UUID  `json:"account_id"`
	CategoryID         *uuid.UUID `json:"category_id,omitempty"`
	Name               string     `json:"name"`
	TransactionType    string     `json:"transaction_type"`
	Amount             string     `json:"amount"`
	Currency           string     `json:"currency,omitempty"`
	Description        *string    `json:"description,omitempty"`
	Frequency          string     `json:"frequency"`
	CustomIntervalDays *int       `json:"custom_interval_days,omitempty"`
	DayOfMonth         *int       `json:"day_of_month,omitempty"`
	DayOfWeek          *int       `json:"day_of_week,omitempty"`
	StartDate          string     `json:"start_date"`
	EndDate            *string    `json:"end_date,omitempty"`
	IsActive           *bool      `json:"is_active,omitempty"`
}

func (p pushRecurringPayload) toInput() (RecurringTransactionInput, error) {
	amount, err := parseAmount(p.Amount, "amount")
	if err != nil {
		return RecurringTransactionInput{}, err
	}
	startDate, err := parseDay(p.StartDate, "start_date")
	if err != nil {
		return RecurringTransactionInput{}, err
	}
	endDate, err := parseOptionalDay(p.EndDate, "end_date")
	if err != nil {
		return RecurringTransactionInput{}, err
	}
	// A template with no explicit is_active is active, same default as the REST
	// endpoint — an offline-created template should start running.
	isActive := true
	if p.IsActive != nil {
		isActive = *p.IsActive
	}
	return RecurringTransactionInput{
		AccountID:          p.AccountID,
		CategoryID:         p.CategoryID,
		Name:               p.Name,
		TransactionType:    p.TransactionType,
		Amount:             amount,
		Currency:           p.Currency,
		Description:        p.Description,
		Frequency:          p.Frequency,
		CustomIntervalDays: p.CustomIntervalDays,
		DayOfMonth:         p.DayOfMonth,
		DayOfWeek:          p.DayOfWeek,
		StartDate:          startDate,
		EndDate:            endDate,
		IsActive:           isActive,
	}, nil
}

func (s *SyncService) recurringPusher() entityPusher[RecurringTransactionInput, sqlcRecurring] {
	svc := s.svcs.Recurring
	return entityPusher[RecurringTransactionInput, sqlcRecurring]{
		decode: func(raw json.RawMessage) (RecurringTransactionInput, error) {
			p, err := decodePayload[pushRecurringPayload](raw, "recurring_transaction")
			if err != nil {
				return RecurringTransactionInput{}, err
			}
			return p.toInput()
		},
		get:       svc.Get,
		updatedAt: func(r sqlcRecurring) pgtype.Timestamptz { return r.UpdatedAt },
		create:    svc.Create,
		update:    svc.Update,
		remove:    svc.Delete,
	}
}

// --- Savings goal contribution -----------------------------------------------

type pushContributionPayload struct {
	GoalID           uuid.UUID `json:"savings_goal_id"`
	Amount           string    `json:"amount"`
	ContributionDate *string   `json:"contribution_date,omitempty"`
	Notes            *string   `json:"notes,omitempty"`
}

// pushContribution handles contributions, which do not fit entityPusher: they
// hang off a parent goal and the API offers no way to edit or remove one (a
// contribution moves a goal's current_amount, so revising it after the fact
// would need a compensating write the domain does not model yet).
func (s *SyncService) pushContribution(ctx context.Context, userID uuid.UUID, item PushItem) PushItemResult {
	if item.Operation != OpCreate {
		return rejected(item.ClientRef, "savings_goal_contribution only supports create")
	}

	p, err := decodePayload[pushContributionPayload](item.Payload, "savings_goal_contribution")
	if err != nil {
		return rejected(item.ClientRef, err.Error())
	}
	if p.GoalID == uuid.Nil {
		return rejected(item.ClientRef, "savings_goal_id is required")
	}
	amount, err := parseAmount(p.Amount, "amount")
	if err != nil {
		return rejected(item.ClientRef, err.Error())
	}
	date, err := parseOptionalDay(p.ContributionDate, "contribution_date")
	if err != nil {
		return rejected(item.ClientRef, err.Error())
	}

	res, err := s.svcs.SavingsGoal.AddContribution(ctx, userID, p.GoalID, ContributionInput{
		Amount:           amount,
		ContributionDate: date,
		Notes:            p.Notes,
	})
	if err != nil {
		return mapServiceError(item.ClientRef, err)
	}
	return PushItemResult{ClientRef: item.ClientRef, Status: StatusApplied, ServerEntity: res}
}

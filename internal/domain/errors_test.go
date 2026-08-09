package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBusinessRule(t *testing.T) {
	// A sample across the entities that flow through /sync/push.
	for _, err := range []error{
		ErrNotFound,
		ErrBudgetExists,
		ErrPeriodClosed,
		ErrAccountHasTransactions,
		ErrInvalidGoalDates,
		ErrInvalidRecurringConfig,
		ErrInvalidTrackingConfig,
	} {
		assert.True(t, IsBusinessRule(err), "%v should be a business rule", err)
	}

	// Wrapped errors must still be recognised — services wrap freely.
	assert.True(t, IsBusinessRule(fmt.Errorf("create budget: %w", ErrBudgetExists)))
}

func TestIsBusinessRuleExcludesOurOwnFaults(t *testing.T) {
	// These are not the caller's doing and may succeed on retry, so push must
	// keep treating them as server errors.
	assert.False(t, IsBusinessRule(ErrAIUnavailable))
	assert.False(t, IsBusinessRule(ErrAIUpstream))
	assert.False(t, IsBusinessRule(errors.New("connection refused")))
	assert.False(t, IsBusinessRule(nil))
}

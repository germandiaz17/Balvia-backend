# Diagrama ER — Balvia (Data Operativa)

## Diagrama de Entidad-Relación

```mermaid
erDiagram
    users ||--o| user_settings : "configura"
    users ||--o{ accounts : "tiene"
    users ||--o{ categories : "personaliza"
    users ||--o{ tracking_periods : "genera"
    users ||--o{ transactions : "registra"
    users ||--o{ budgets : "define"
    users ||--o{ savings_goals : "persigue"
    users ||--o{ recurring_transactions : "programa"

    tracking_periods ||--|| tracking_period_summaries : "se cierra con (snapshot)"
    tracking_periods ||--o{ tracking_period_insights : "genera"
    tracking_periods ||--o{ transactions : "contiene"
    tracking_periods ||--o{ budgets : "delimita"
    tracking_periods ||--o{ savings_goal_contributions : "registra aportes en"

    accounts ||--o{ transactions : "origen/destino"
    accounts ||--o{ recurring_transactions : "afecta"
    accounts |o--o| savings_goals : "asociada opcional"

    categories ||--o{ categories : "subcategoría de"
    categories ||--o{ transactions : "clasifica"
    categories ||--o{ budgets : "limita gasto en"
    categories ||--o{ recurring_transactions : "categoriza"

    savings_goals ||--o{ savings_goal_contributions : "recibe"
    transactions |o--o| savings_goal_contributions : "puede originar"

    users {
        UUID id PK
        VARCHAR email UK
        VARCHAR full_name
        TIMESTAMPTZ created_at
        TIMESTAMPTZ deleted_at
    }

    user_settings {
        UUID id PK
        UUID user_id FK
        SMALLINT tracking_start_day
        SMALLINT tracking_duration_days
        CHAR default_currency
        VARCHAR theme
        VARCHAR default_period_view
        VARCHAR subscription_tier
    }

    accounts {
        UUID id PK
        UUID user_id FK
        VARCHAR name
        VARCHAR account_type
        CHAR currency
        NUMERIC initial_balance
        NUMERIC current_balance
        BOOLEAN is_archived
    }

    categories {
        UUID id PK
        UUID user_id FK
        UUID parent_id FK
        VARCHAR name
        VARCHAR category_type
        BOOLEAN is_system
    }

    tracking_periods {
        UUID id PK
        UUID user_id FK
        DATE start_date
        DATE end_date
        VARCHAR status
        INT sequence_number
        TIMESTAMPTZ closed_at
    }

    transactions {
        UUID id PK
        UUID user_id FK
        UUID tracking_period_id FK
        UUID account_id FK
        UUID category_id FK
        VARCHAR transaction_type
        NUMERIC amount
        DATE transaction_date
        BOOLEAN ai_categorized
        BOOLEAN voice_input
        VARCHAR client_id
    }

    budgets {
        UUID id PK
        UUID user_id FK
        UUID tracking_period_id FK
        UUID category_id FK
        NUMERIC amount
        NUMERIC alert_threshold_warning
    }

    savings_goals {
        UUID id PK
        UUID user_id FK
        VARCHAR name
        NUMERIC target_amount
        NUMERIC current_amount
        DATE start_date
        DATE target_date
        VARCHAR status
    }

    savings_goal_contributions {
        UUID id PK
        UUID savings_goal_id FK
        UUID user_id FK
        UUID tracking_period_id FK
        UUID transaction_id FK
        NUMERIC amount
        DATE contribution_date
    }

    recurring_transactions {
        UUID id PK
        UUID user_id FK
        UUID account_id FK
        UUID category_id FK
        VARCHAR name
        NUMERIC amount
        VARCHAR frequency
        DATE next_due_date
        BOOLEAN is_active
    }

    tracking_period_summaries {
        UUID id PK
        UUID tracking_period_id FK
        UUID user_id FK
        NUMERIC total_income
        NUMERIC total_expenses
        NUMERIC net_savings
        NUMERIC savings_rate
        JSONB expense_by_category
        JSONB budget_performance
        JSONB vs_previous_period
    }

    tracking_period_insights {
        UUID id PK
        UUID tracking_period_id FK
        UUID user_id FK
        VARCHAR insight_type
        VARCHAR calculation_phase
        VARCHAR severity
        VARCHAR title
        JSONB data
        BOOLEAN is_dismissed
    }
```

## Vista jerárquica de dominios

```
                  ┌─────────────────┐
                  │     users       │  (módulo Auth - placeholder)
                  └────────┬────────┘
                           │
            ┌──────────────┼──────────────┐
            │              │              │
            ▼              ▼              ▼
   ┌────────────────┐ ┌─────────┐ ┌─────────────┐
   │ user_settings  │ │accounts │ │ categories  │  (globales, no atadas a periodo)
   └────────────────┘ └─────────┘ └─────────────┘
            │              │              │
            ▼              │              │
   ┌──────────────────────────────────────────────┐
   │      tracking_periods (raíz temporal)        │
   └──────────┬──────────────────┬────────────────┘
              │                  │
              ├──────────────────┼────────────────┐
              ▼                  ▼                ▼
   ┌──────────────────┐ ┌──────────────┐ ┌──────────────────┐
   │   transactions   │ │   budgets    │ │   summaries +    │
   │                  │ │              │ │   insights       │
   └──────────────────┘ └──────────────┘ └──────────────────┘
              │
              ▼
   ┌──────────────────────┐    ┌────────────────────┐
   │ savings_goal_        │◄──►│   savings_goals    │ (independientes)
   │ contributions        │    └────────────────────┘
   └──────────────────────┘

   ┌─────────────────────────────────┐
   │   recurring_transactions        │ (independientes, generan transactions)
   └─────────────────────────────────┘
```

## Cardinalidades clave

| Relación | Cardinalidad | Nota |
|----------|--------------|------|
| user → tracking_periods | 1:N | Solo 1 activo a la vez (unique partial index) |
| user → user_settings | 1:1 | Configuración única por usuario |
| tracking_period → transactions | 1:N | Restrict on delete (no se puede borrar un periodo con transacciones) |
| tracking_period → summary | 1:1 | Solo al cerrar el periodo |
| tracking_period → insights | 1:N | Múltiples insights por periodo |
| tracking_period → budgets | 1:N | Presupuestos por categoría, replicados cada periodo |
| account → transactions | 1:N | Restrict on delete |
| category → transactions | 1:N | Set NULL on delete (preserva la transacción) |
| savings_goal → contributions | 1:N | Aportes desde múltiples periodos |
| transaction → contribution | 0..1:1 | Una transacción puede originar un aporte |
```

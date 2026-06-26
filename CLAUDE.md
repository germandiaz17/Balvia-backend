# Balvia Backend — Contexto del Proyecto

> **Documento de contexto para Claude Code**
> Este archivo describe el proyecto, las decisiones tomadas, el stack técnico y el estado actual del desarrollo. Léelo completo antes de proponer cualquier cambio.

---

## 1. Visión del producto

**Balvia** es una aplicación móvil de gestión de finanzas personales con IA integrada, orientada al mercado colombiano.

### Filosofía UX
- Registro ultra-rápido de gastos (<5 segundos) vía modal flotante
- Input por voz con transcripción automática
- Categorización automática con IA
- Offline-first con sincronización

### Modelo de negocio
Freemium: Free / Pro ($9.99 USD) / Premium ($24.99 USD) / Business ($99.99 USD)

---

## 2. Stack técnico

### Backend (este repo)
- **Lenguaje**: Go 1.26+
- **Framework HTTP**: Fiber v2 (sintaxis tipo Express, basado en fasthttp)
- **Base de datos**: PostgreSQL 15+ (Neon como provider managed)
- **SQL/Queries**: sqlx + sqlc (NO usar GORM)
- **Migraciones**: golang-migrate
- **Validación**: go-playground/validator
- **Auth**: golang-jwt/jwt
- **Logging**: zerolog
- **Config**: viper o godotenv
- **UUID**: google/uuid
- **Decimal (dinero)**: shopspring/decimal
- **Testing**: testify

### Mobile (repo separado: balvia-mobile)
- Flutter + Dart
- BD local: SQLite (Drift o sqflite)
- State management: BLoC o Provider
- Solo Android para MVP (iOS futuro)

### Infraestructura
- **Staging/Producción inicial**: Hetzner VPS (~$4-5/mes)
- **BD**: Neon free tier para MVP
- **IA**: Hugging Face Inference API (free tier)

---

## 3. Decisión arquitectónica clave: el "seguimiento"

El concepto central de Balvia es el **tracking_period** (seguimiento). Es la unidad temporal raíz alrededor de la cual orbita toda la data operativa.

### Reglas inmutables del seguimiento
1. Es la **raíz temporal** de toda la data operativa
2. **Solo 1 seguimiento activo por usuario** en cualquier momento (garantizado a nivel BD con partial unique index)
3. **Duración: 28-31 días** (CHECK constraint en BD)
4. **Generación automática** al cerrar uno (no manual)
5. **Inmutable al cerrar**: el seguimiento pasado no se toca, incluyendo sus transacciones
6. **Toda transacción pertenece a 1 y solo 1 seguimiento**
7. **No se pueden registrar transacciones con fecha pasada** (solo en el seguimiento activo, con fecha actual)
8. **Cambios en configuración de fecha** aplican solo al siguiente seguimiento
9. **NO se permite cerrar manualmente el seguimiento activo** (por ahora)
10. Los seguimientos del mismo usuario **NUNCA pueden traslaparse en fechas** (EXCLUDE constraint con btree_gist)

### Configuración del usuario (user_settings)
- `tracking_start_day` (día del mes en que arranca): 1-31
- `tracking_duration_days` (28-31)

### Vistas del seguimiento (UI only, no BD)
- **Vista completa**: todo el seguimiento (default)
- **Vista quincenal**: 2 sub-bloques (15 días c/u)
- **Vista semanal**: 4 sub-bloques

Se calculan on-the-fly desde transacciones. No se almacenan sub-periodos.

### Relación con presupuestos
- Cada presupuesto está atado a un `tracking_period_id`
- Al crear el siguiente seguimiento, **los presupuestos se auto-copian** del anterior

### Relación con metas de ahorro
- Las metas **NO están atadas a un seguimiento** (son largo plazo)
- Las metas tienen su propio rango de fechas
- Al calcular progreso, se buscan los `tracking_periods` que intersecan el rango

---

## 4. Esquema de base de datos

### Tablas (12 en total, módulo "Data Operativa")

1. **users** — Placeholder mínimo (el módulo de Auth se diseñará separado)
2. **user_settings** — Config de seguimientos + preferencias
3. **accounts** — Cuentas financieras (cash, checking, savings, credit_card, investment, other)
4. **categories** — Sistema + personalizadas, jerárquicas con parent_id
5. **tracking_periods** — La raíz temporal del sistema
6. **transactions** — Movimientos (income, expense, transfer) atadas a tracking_period
7. **budgets** — Presupuestos por categoría dentro de un tracking_period
8. **savings_goals** — Metas de ahorro (independientes del periodo)
9. **savings_goal_contributions** — Aportes a metas
10. **recurring_transactions** — Plantillas de transacciones recurrentes
11. **tracking_period_summaries** — Snapshot al cerrar (1:1 con tracking_periods cerrados)
12. **tracking_period_insights** — Análisis inteligentes (durante + final)

### Convenciones
- UUIDs como PK (`gen_random_uuid()`)
- `NUMERIC(15,2)` para todo lo monetario
- `TIMESTAMPTZ` para fechas con timezone
- Soft deletes con `deleted_at` (users, accounts, categories, transactions, savings_goals, recurring_transactions)
- snake_case para tablas y columnas
- Triggers automáticos para `updated_at`
- Trigger crítico: `validate_transaction_period` en transactions (verifica fecha en rango y periodo no cerrado)

### Insights del sistema

**Durante el seguimiento** (calculados por evento o lazy):
- spending_pace, budget_warning, budget_exceeded, ant_expenses_early, unusual_expense, vs_previous_partial, goal_progress_alert

**Al cerrar el seguimiento** (inmutables):
- top_categories, top_merchants, ant_expenses_final, reduction_opportunity, savings_summary, budget_compliance, vs_previous_final, monthly_wrap_up, goal_achievement_summary

**Estrategia de cálculo**:
- Al crear transacción → recalcular insights inmediatos (presupuestos, ritmo)
- Al abrir dashboard → refrescar insights "durante" más pesados (lazy)
- Al cerrar seguimiento → calcular todos los "final" + crear summary

### Cierre automático de seguimientos
- **Job programado nocturno** (cron en backend Go) — cierra los seguimientos que llegaron a su fin
- **Cierre perezoso al abrir la app** (fallback) — si el backend falló

---

## 5. Estructura del proyecto

```
balvia-backend/
├── cmd/
│   └── api/
│       └── main.go              # Punto de entrada
├── internal/
│   ├── config/                  # Carga de .env
│   ├── database/                # Conexión a Postgres, migrations runner
│   ├── domain/
│   │   ├── models/              # Structs de entidades
│   │   └── repositories/        # Interfaces de acceso a datos
│   ├── handlers/                # Endpoints HTTP (Fiber routes)
│   ├── services/                # Lógica de negocio
│   ├── middleware/              # Auth, logger, CORS
│   └── utils/                   # Helpers
├── migrations/                  # Archivos .sql para golang-migrate
├── pkg/
│   └── logger/                  # Logger configurado (reutilizable)
├── scripts/                     # Bash scripts (deploy, db reset)
├── docs/                        # OpenAPI specs, docs internas
├── go.mod
├── .env.example
├── .gitignore
├── README.md
└── Makefile
```

### ¿Por qué `internal/`?
Go no permite que otros módulos importen lo que está dentro de `internal/`. Esto fuerza buen encapsulamiento.

### ¿Por qué `pkg/` aparte?
Cosas que potencialmente podrían ser reutilizables por otros proyectos.

---

## 6. Estado actual del desarrollo

> **Nota de stack resuelta**: módulo Go = `github.com/germandiaz17/Balvia-backend` (B mayúscula, intencional). Elecciones confirmadas: Fiber **v2**, **godotenv** (no viper), driver **pgx v5**, CLI (`sqlc`, `migrate`) vía `go install` global. Patrón de capa de datos: **Store + execTx** (el `Store` embebe `sqlc.Querier` y añade métodos transaccionales); capas `handler → service → store`.

### ✅ Completado
- Decisión de stack (Go + Fiber + sqlc + golang-migrate + PostgreSQL/Neon)
- Decisión de arquitectura (Clean Architecture + concept de tracking_period)
- Diseño completo de BD operativa (12 tablas, 34 índices, 11 triggers)
- Instalación de Go 1.26.3 en sistema Arch Linux (Omarchy) + env (GOPATH, PATH)
- **Estructura de carpetas creada** (sección 5)
- **Dependencias instaladas** (Fiber v2, sqlx, pgx v5, validator, jwt, zerolog, godotenv, uuid, decimal, testify; CLI `sqlc` + `migrate`)
- **`.env`/`.env.example`/`.gitignore`/`Makefile`/`sqlc.yaml` configurados**
- **Migraciones adaptadas a golang-migrate** (12 pares `000001…000012`, desde el formato Diesel)
- **Migraciones aplicadas a Neon** (PG18, región us-east-1; pooled p/ app, directa p/ migrate)
- **Modelos Go generados con sqlc** + pool pgx (`internal/database`)
- **Health check** (`/health` liveness + `/health/ready` con ping a BD)
- **Flujo de onboarding** (`POST /api/v1/onboarding`): crea user + user_settings + primer tracking_period atómicamente, con tests
- **CRUD de transacciones** (`/api/v1/transactions`): Create + List + Get + **Update** + Delete(soft). Valida contra el periodo activo (fecha en rango, periodo no cerrado vía trigger), ownership de cuenta/categoría, y **actualiza `current_balance` atómicamente** (income/expense/transfer; revierte en delete; reverse+reaplica en update). Con tests.
- **CRUD de cuentas** (`/api/v1/accounts`) y **categorías** (`/api/v1/categories`, sistema read-only + propias del usuario).
- **Cierre de seguimientos (paso 11)**: `ClosePeriodTx` atómico = cierra periodo + snapshot `tracking_period_summary` (totales core: income/expense/transfer, net_savings, savings_rate, conteos, top categoría) + genera el siguiente periodo contiguo + copia budgets. **Scheduler in-process** (cada hora) + **cierre perezoso** al resolver el periodo activo. Con tests E2E.
- **Auth (paso 12)**: JWT access token + refresh token rotado (bcrypt, tabla `refresh_tokens`, migración 000013). Endpoints `/api/v1/auth/{register,login,refresh,logout,me}`. `register` reemplaza al onboarding público (crea user+settings+periodo+tokens). Middleware `JWTAuth` protege las rutas (reemplazó al stub `X-User-ID`). Con E2E.

> **Contrato de API**: documentado en `../docs/API_CONTRACT.md` (fuente de verdad back⇆front). Actualízalo en el MISMO cambio en que toques un endpoint. Estado/backlog en `../docs/ROADMAP.md`.

### 🔄 En progreso
- (siguiente) Budgets / metas de ahorro / recurrentes (CRUD), o deploy

### ⏭️ Próximos pasos / pendientes conocidos
- **Insights "final" ricos** (los 9 tipos: reduction_opportunity, vs_previous, top_merchants...) y breakdowns JSONB del summary (expense_by_category/day, budget_performance) → paso de analítica aparte
- **CRUD de budgets, savings_goals, recurring_transactions** (tablas listas, sin endpoints aún)
- **Insights "during"** (spending_pace, budget_warning...) en tiempo real
12. Diseñar módulo de Auth aparte (reemplaza el middleware stub `X-User-ID` por JWT)

---

## 7. Migraciones SQL existentes

Las migraciones fueron diseñadas inicialmente para Diesel CLI (Rust), con esta estructura:

```
migrations/
├── 2026-06-24-000001_initial_setup/
│   ├── up.sql
│   └── down.sql
├── 2026-06-24-000002_users_placeholder/
│   ├── up.sql
│   └── down.sql
... (12 carpetas en total)
```

**Necesitan adaptarse al formato de golang-migrate**, que es:

```
migrations/
├── 000001_initial_setup.up.sql
├── 000001_initial_setup.down.sql
├── 000002_users_placeholder.up.sql
├── 000002_users_placeholder.down.sql
...
```

Las migraciones cubren (en orden):
1. Extensions (pgcrypto, btree_gist)
2. users (placeholder)
3. user_settings
4. accounts + categories
5. tracking_periods (con EXCLUDE constraint para no-overlap)
6. transactions (con trigger validate_transaction_period)
7. budgets
8. savings_goals + savings_goal_contributions
9. recurring_transactions
10. tracking_period_summaries + tracking_period_insights
11. triggers updated_at (función + 10 triggers)
12. seed system categories (20 categorías predefinidas para Colombia)

---

## 8. Convenciones de código Go a seguir

### General
- Sigue `gofmt` siempre (formateo automático)
- Sigue convenciones idiomáticas de Go (effective Go)
- Errores: returnea explícitamente, NO uses panic excepto en main
- Nombres cortos pero claros: `db`, `ctx`, `txn` están bien si el scope es chico
- Variables/funciones en `camelCase`, exportadas en `PascalCase`

### Estructura de handlers (Fiber)
```go
func CreateTransaction(c *fiber.Ctx) error {
    var input CreateTransactionInput
    if err := c.BodyParser(&input); err != nil {
        return fiber.NewError(fiber.StatusBadRequest, err.Error())
    }
    if err := validate.Struct(input); err != nil {
        return fiber.NewError(fiber.StatusBadRequest, err.Error())
    }
    // delegar a service
    txn, err := transactionService.Create(c.Context(), input)
    if err != nil {
        return err
    }
    return c.Status(fiber.StatusCreated).JSON(txn)
}
```

### Repositorios (interfaces)
```go
type TransactionRepository interface {
    Create(ctx context.Context, txn *models.Transaction) error
    FindByID(ctx context.Context, id uuid.UUID) (*models.Transaction, error)
    // ...
}
```

### Manejo de dinero
- **SIEMPRE** usa `shopspring/decimal.Decimal` para campos monetarios
- **NUNCA** uses `float64` para dinero

### Manejo de errores
- Define errores de dominio en `internal/domain/errors.go`
- En handlers, mapea a status codes HTTP
- Loggea con zerolog incluyendo request_id para trazabilidad

---

## 9. Información del desarrollador

- **Nombre**: Germán
- **GitHub**: germandiaz17
- **Sistema**: Arch Linux (Omarchy + Hyprland) en ThinkPad T470
- **Editor principal**: por definir (probablemente Neovim, VS Code o Zed)
- **Idioma de preferencia**: Español para conversación, inglés para código y comentarios
- **Experiencia**: 5+ años fullstack (FastAPI/Python, Angular, Rust). Go es nuevo pero conoce conceptos.
- **Contexto laboral**: Trabaja en Skanhawk SAS (Colombia) en proyectos de IoT industrial (SkanMonitor, SkanView)

### Preferencias de comunicación
- Estilo directo y técnico
- Prefiere explicaciones con razonamiento ("por qué") sobre solo el "qué"
- Le gusta entender decisiones de arquitectura antes de codear
- Hace preguntas comparativas (X vs Y) para validar decisiones
- Prefiere validar conceptos antes de seguir con código

---

## 10. Referencias importantes

### Documentos de diseño (en archivos separados que el usuario te puede compartir)
- `schema.sql` — DDL completo del módulo operativo
- `diagram.md` — Diagrama ER en Mermaid
- `design_decisions.md` — Decisiones de diseño detalladas

### Repos
- Backend: `github.com/germandiaz17/balvia-backend`
- Mobile: `github.com/germandiaz17/balvia-mobile`

---

## 11. Reglas para Claude Code en este proyecto

1. **No uses GORM**. Usa sqlx + sqlc.
2. **No uses float para dinero**. Siempre `shopspring/decimal.Decimal`.
3. **No invoques panic** excepto en main si la app no puede arrancar.
4. **Respeta la arquitectura de capas**: handler → service → repository. No metas SQL en handlers.
5. **Mantén la convención del tracking_period**: toda operación de transacciones, presupuestos e insights debe respetar las reglas del seguimiento activo.
6. **Migraciones**: usa formato de golang-migrate. NO modifiques migraciones ya aplicadas, crea nuevas.
7. **Tests**: para lógica de negocio crítica (servicios), escribe tests con testify.
8. **Logs estructurados**: usa zerolog. Incluye `request_id` en cada log de request.
9. **Errores de dominio**: defínelos en `internal/domain/errors.go` y mapéalos en handlers.
10. **Pregunta antes de tomar decisiones grandes** de arquitectura. El desarrollador prefiere validar conceptos.

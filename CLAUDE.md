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
3. **Duración: 28-31 días** (CHECK constraint en BD). Única excepción: los **periodos de
   transición** (`is_transition`), que nacen al cambiar de modo y pueden medir 15-45 días.
4. **Generación automática** al cerrar uno (no manual)
5. **Inmutable al cerrar**: el seguimiento pasado no se toca, incluyendo sus transacciones
6. **Toda transacción pertenece a 1 y solo 1 seguimiento**
7. **No se pueden registrar transacciones con fecha pasada** (solo en el seguimiento activo, con fecha actual)
8. **Cambios en configuración de fecha** aplican solo al siguiente seguimiento
9. **NO se permite cerrar manualmente el seguimiento activo** (por ahora)
10. Los seguimientos del mismo usuario **NUNCA pueden traslaparse en fechas** (EXCLUDE constraint con btree_gist)

### Modos de seguimiento (`tracking_period_mode`)
- **`rolling`** (default): bloques de `tracking_duration_days` encadenados. Comportamiento histórico.
- **`calendar_month`**: meses reales, del día 1 al último día. `tracking_duration_days` queda inerte.

Al pasar de `rolling` a `calendar_month`, el hueco hasta el día 1 se cubre con un **puente**
(`is_transition = true`): si quedan ≥15 días de mes es el resto de ese mes, si no se extiende al fin
del mes siguiente. Siempre mide 15-45 días y siempre termina en frontera de mes. El cambio inverso
nunca necesita puente. Toda la aritmética vive en `internal/domain/period.go` (`NextPeriodRange` /
`FirstPeriodRange`), función pura y con tests exhaustivos.

**Excepción a la regla 8**: si el periodo activo es el #1 y no tiene transacciones, cambiar el modo
lo reforma en sitio (`UserSettingsService.reshapePristineFirstPeriod`). Es para el wizard de
onboarding, que corre después del registro. No hay nada que invalidar, así que no hay razón para
diferirlo.

### Configuración del usuario (user_settings)
- `tracking_start_day` (día del mes en que arranca): 1-31
- `tracking_duration_days` (28-31)
- `tracking_period_mode` (`rolling` | `calendar_month`)

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
- **Migraciones adaptadas a golang-migrate** (hoy **18 pares** `000001…000018`, ver sección 7)
- **Migraciones aplicadas a Neon** (PG18, región us-east-1; pooled p/ app, directa p/ migrate)
- **Modelos Go generados con sqlc** + pool pgx (`internal/database`)
- **Health check** (`/health` liveness + `/health/ready` con ping a BD)
- **Flujo de onboarding** (`POST /api/v1/onboarding`): crea user + user_settings + primer tracking_period atómicamente, con tests
- **CRUD de transacciones** (`/api/v1/transactions`): Create + List + Get + **Update** + Delete(soft). Valida contra el periodo activo (fecha en rango, periodo no cerrado vía trigger), ownership de cuenta/categoría, y **actualiza `current_balance` atómicamente** (income/expense/transfer; revierte en delete; reverse+reaplica en update). Con tests.
- **CRUD de cuentas** (`/api/v1/accounts`) y **categorías** (`/api/v1/categories`, sistema read-only + propias del usuario).
- **Cierre de seguimientos (paso 11)**: `ClosePeriodTx` atómico = cierra periodo + snapshot `tracking_period_summary` (totales core: income/expense/transfer, net_savings, savings_rate, conteos, top categoría) + genera el siguiente periodo contiguo + copia budgets. **Scheduler in-process** (cada hora) + **cierre perezoso** al resolver el periodo activo. Con tests E2E.
- **Auth (paso 12)**: JWT access token + refresh token rotado (bcrypt, tabla `refresh_tokens`, migración 000013). Endpoints `/api/v1/auth/{register,login,refresh,logout,me}`. `register` reemplaza al onboarding público (crea user+settings+periodo+tokens). Middleware `JWTAuth` protege las rutas (reemplazó al stub `X-User-ID`). Con E2E.

> **Contrato de API**: `docs/API_CONTRACT.md` (fuente de verdad back⇆front). Actualízalo en el MISMO
> cambio en que toques un endpoint. Estado/backlog: `docs/ROADMAP.md`. La documentación narrativa
> vive en el vault de Obsidian (`~/Documents/Balvia Brain/`).

- **CRUD de budgets (paso 13)**: `/api/v1/budgets` (Create/List/Get/Update/Delete). Atados al **periodo activo** al crear (lazy-close incluido); `category_id` opcional (null = presupuesto global); **único por (periodo, categoría)** → 409 vía `ErrBudgetExists`; umbrales de alerta 0–100 (default 80/100); presupuestos de periodos **cerrados son inmutables** (`ErrPeriodClosed`, 422) en update/delete. Hard delete (sin `deleted_at`). Con tests de servicio.
- **IA — categorización automática (backend, BYOK multi-proveedor)**: cada usuario trae **su propia API key** (Anthropic o cualquier endpoint **OpenAI-compatible** vía `base_url`), guardada **cifrada** (AES-256-GCM). Piezas:
  - `internal/ai`: interface `Categorizer` + adaptadores `AnthropicCategorizer` (SDK `anthropic-sdk-go`, tool-use forzado con `enum` de ids) y `OpenAICategorizer` (`net/http` a `/chat/completions`, function calling; cubre OpenAI/Groq/OpenRouter/Ollama…); `Build()` selecciona por proveedor. `Service.Categorize` carga las settings del usuario, **descifra la key**, construye el cliente, filtra candidatos por tipo (default `expense`) y **rechaza ids alucinados**. `SettingsService` gestiona el CRUD (key **write-only**, nunca vuelve en respuestas).
  - `internal/crypto`: `AESGCM` (encrypt/decrypt) con tests. Config `AI_ENCRYPTION_KEY` (64 hex/32 bytes, **opcional** → sin ella /ai/* da **503**).
  - Tabla **`user_ai_settings`** (migración **000017**, 1:1 con users, `provider` CHECK, `api_key_encrypted BYTEA`, `base_url`, `model`, `enabled`). sqlc regenerado.
  - Endpoints (todos Bearer): `PUT/GET/DELETE /api/v1/ai/settings` + `POST /api/v1/ai/categorize`. Errores: 503 (sin cifrado server), 422 (`ErrAINotConfigured`/`ErrInvalidAIProvider`), 502 (`ErrAIUpstream` — key inválida/proveedor caído).
  - Verificado: unit tests (`ai` + `crypto`) + E2E completo (ciclo settings, key no se filtra, cadena descifrar→construir→llamar al proveedor real).
  - **Metadata de IA en transacciones**: `transactions.ai_categorized`/`ai_confidence`/`ai_suggested_category_id` (columnas ya existían desde 000006) ahora se **setean en el create** (query sqlc `CreateTransaction` + `CreateTransactionInput` + handler `createTxnRequest`/response) y viajan por **sync push** (`PushTransactionPayload` + `txnPayloadToInput`) **y pull** (`syncTransactionResponse`). Confidence en el wire como string decimal. Verificado E2E (create/push/pull). Mobile: pantalla `/ai-settings` + "Sugerir con IA" en captura + persistencia (Drift v2). **Pendiente**: verificación visual en emulador con una key real.

- **CRUD de metas de ahorro**: `/api/v1/savings-goals` (Create/List/Get/Update/Delete) + `POST/GET /:id/contributions`. `CreateContributionTx` actualiza `current_amount` y marca `achieved` **atómicamente**, resolviendo el periodo activo (con lazy-close). El `PUT` es **replace completo** (`name`, `target_amount`, `target_date`, `status` requeridos; **no** acepta `start_date`). Con tests de servicio.
- **CRUD de transacciones recurrentes**: `/api/v1/recurring-transactions` (plantillas) + **motor de materialización** (`RecurringEngineService`) con **scheduler in-process horario** y catch-up. `computeNextDueDate` avanza `next_due_date`; al agotarse una plantilla (`end_date` alcanzado) queda `next_due_date = NULL` e `is_active = false`. ⚠️ `GET /recurring-transactions` **tiene efecto secundario**: dispara `ProcessUserRecurring` antes de responder, así que puede crear transacciones reales. Con tests de validación y del motor.
- **Tracking periods read-only**: `GET /api/v1/tracking-periods` (list), `/active`, `/:id`, `/:id/summary?view=` (full|biweekly|weekly).
- **Subsistema de insights**: 16 tipos — 7 "during" (`analytics_during.go`, recalculados best-effort al crear transacción y de forma perezosa al consultar) y 9 "final" (`analytics.go`, generados e inmutables al cierre). Expuestos en `GET /tracking-periods/:id/insights`, polimórfico por estado del periodo. 37 tests entre ambos generadores.
- **Motor de sync offline-first**: `GET /api/v1/sync/pull` (delta por cursor `since`, paginado, 8 entidades, soft-deletes) + `POST /api/v1/sync/push` (por lotes, idempotente por `client_id`, rechazo por ítem).
- **Push multi-entidad (2026-08-08)**: el push acepta las **7 entidades** (transaction, account, category, budget, savings_goal, recurring_transaction y savings_goal_contribution, esta última solo `create`). La forma común — decodificar, llamar al servicio, comparar `updated_at`, armar el resultado — vive una sola vez en `entityPusher` (`internal/services/sync_push_entities.go`); cada entidad aporta solo lo que de verdad difiere. Push **no tiene lógica de negocio propia**: replica los mismos servicios que usan los endpoints REST, así que una edición offline no puede colarse por debajo de una regla.

- **Configuración del usuario**: `GET/PUT /api/v1/settings` (recurso singleton). Update **parcial** vía `COALESCE(sqlc.narg(...), columna)` — una clave ausente deja la columna intacta. Valida rangos (28–31, 1–31) y enums (theme, period view, currency 3 letras mayúsculas) **antes** de la BD, así que un CHECK de Postgres nunca aflora como 500. Devuelve `applies_to_next_period` + `active_period_end_date` para que la app diga la fecha exacta en que aplica el cambio. Sin migración nueva. Con tests.

- **Onboarding de usuario nuevo**: `Onboard` provisiona además una **cuenta por defecto** (`Efectivo`, `cash`, `COP`, saldo 0, icono `wallet`) dentro de la misma transacción que el user, las settings y el primer periodo. Sin ella el usuario nuevo no podía registrar ni un gasto, porque `account_id` es obligatorio en `POST /transactions`. Para que el wizard de la app pueda fijar el saldo real sin borrar y recrear la cuenta, `PUT /accounts/:id` acepta ahora un `initial_balance` **opcional**: ausente deja el saldo de apertura intacto; presente lo restablece y desplaza `current_balance` por el mismo delta, pero **solo si la cuenta no tiene movimientos** (si los tiene → 422 `ErrAccountHasTransactions`). Query nueva `CountTransactionsByAccount` (cuenta también los traslados donde la cuenta es contracuenta). Sin migración. Con tests (`services/account_test.go`).

- **Modo de seguimiento por mes calendario (2026-08-08)**: `user_settings.tracking_period_mode`
  (`rolling` | `calendar_month`) + `tracking_periods.config_period_mode`/`is_transition`
  (migración **000018**, que además relaja `chk_duration_range` condicionalmente para los puentes).
  `ClosePeriodTx` delega el cálculo de fechas en `domain.NextPeriodRange`. Los presupuestos se
  **prorratean** al entrar o salir de un puente (`budgetProrationFactor`), y `vs_previous_final` /
  `vs_previous_partial` **no se generan** cuando alguno de los periodos comparados es un puente,
  porque comparan totales crudos sin normalizar por día. Verificado E2E contra Neon: carve-out,
  diferimiento, nacimiento del puente, prorrateo (300.000 → 280.000) y mes limpio posterior.

### 🔄 En progreso
- Nada. El hito "modo de seguimiento por mes calendario" quedó cerrado el 2026-08-08.

### ⏭️ Próximos pasos / pendientes conocidos

Detalle completo en `docs/ROADMAP.md`.

- **Los usuarios registrados antes del 2026-08-03 no tienen cuenta por defecto**: la garantía es del registro, no retroactiva.
- **El outbox de la app solo encola transacciones**: el servidor ya acepta las 7 entidades, pero la app todavía muta lo demás por red. Cerrar eso es trabajo del lado Flutter (columna `sync_status` en las tablas Drift que faltan + extender el outbox).
- **`tracking_start_day` es inerte**: `ClosePeriodTx` genera el siguiente periodo como `fin_anterior + 1 día` y solo estampa el valor como metadata. Hacer real el ancla de día del mes exige absorber el desfase en la duración (el CHECK 28–31 solo permite ±3 días/ciclo) → hito propio.
- **Breakdowns JSONB ricos del summary**: faltan `expense_by_day` y `budget_performance` en `tracking_period_summaries`.
- **Tests faltantes**: `internal/handlers/` (nivel HTTP y `mapDomainError`), `internal/auth/` (JWT + bcrypt), `internal/middleware/` (`JWTAuth`), `services/auth.go` (rotación de refresh), `services/{account,category,period}.go`, y `database/store*.go` (la lógica transaccional; el prorrateo de presupuestos sí tiene tests). Todos los tests actuales son unitarios con mocks: los triggers, el EXCLUDE constraint y los CHECKs de Postgres nunca se ejercitan.
- **Infra**: sin CORS, rate limiting ni security headers; sin paginación ni filtros en los listados; sin OpenAPI; sin CI ni Dockerfile; deploy a VPS pendiente.

---

## 7. Migraciones SQL existentes

Ya están en formato **golang-migrate** (un par `.up.sql`/`.down.sql` por versión, plano en
`migrations/`) y **las 18 están aplicadas en Neon**:

```
migrations/
├── 000001_initial_setup.up.sql
├── 000001_initial_setup.down.sql
...
└── 000018_tracking_period_mode.down.sql
```

Cubren (en orden):

| # | Migración | Contenido |
|---|---|---|
| 000001 | initial_setup | Extensions (pgcrypto, btree_gist) |
| 000002 | users_placeholder | users |
| 000003 | user_settings | + CHECKs `tracking_duration BETWEEN 28 AND 31` y `tracking_start_day BETWEEN 1 AND 31` |
| 000004 | accounts_and_categories | |
| 000005 | tracking_periods | EXCLUDE constraint para no-solape + índice único parcial del activo |
| 000006 | transactions | trigger `validate_transaction_period`; ya incluye las columnas `ai_*` |
| 000007 | budgets | |
| 000008 | savings_goals | + savings_goal_contributions |
| 000009 | recurring_transactions | |
| 000010 | summaries_and_insights | tracking_period_summaries + tracking_period_insights |
| 000011 | triggers_updated_at | función + triggers `set_updated_at_*` |
| 000012 | seed_system_categories | 20 categorías predefinidas para Colombia |
| 000013 | auth | `users.password_hash` + tabla `refresh_tokens` |
| 000014 | recurring_engine | soporte del motor de materialización |
| 000015 | recurring_occurrence_date | |
| 000016 | sync_indexes | índices para el delta sync |
| 000017 | user_ai_settings | BYOK: key cifrada por usuario |
| 000018 | tracking_period_mode | modo rodante/calendario + `is_transition` + CHECK de duración condicional |

`make migrate-version` debe reportar **18**.

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

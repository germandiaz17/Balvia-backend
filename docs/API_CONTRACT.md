# Balvia — Contrato de API (back ⇆ front)

> **Fuente de verdad** del contrato entre `Balvia-backend` y `Balvia-movile`.
> **Regla de oro**: si tocas un endpoint, actualiza este archivo **en el mismo cambio**.
>
> Generado a partir de los handlers reales en `internal/handlers/`. Última revisión: **2026-08-01**.

---

## 1. Generalidades

| | |
|---|---|
| **Base URL (dev)** | `http://localhost:8080/api/v1` — emulador Android: `http://10.0.2.2:8080/api/v1` |
| **Auth** | `Authorization: Bearer <access_token>` en todo salvo `/health*` y `/auth/{register,login,refresh,logout}` |
| **Formato** | JSON. `Content-Type: application/json` |
| **Dinero** | **Siempre string decimal** (`"1500000.00"`). Nunca float. Ojo: el backend responde con `decimal.String()`, que **recorta ceros a la derecha** → puede llegar `"3000000"` y no `"3000000.00"`. El cliente debe parsear con un decimal, no comparar strings. |
| **Fechas solo-día** | `YYYY-MM-DD` |
| **Timestamps** | RFC3339 UTC (`2026-08-01T02:09:08Z`) |
| **Errores** | `{"error": "mensaje"}` |

### Códigos de estado

| Código | Cuándo |
|---|---|
| `400` | Body malformado o que no pasa las tags del validator (formato) |
| `401` | Sin token, token inválido o expirado |
| `404` | El recurso no existe o no es del usuario |
| `409` | Conflicto (p. ej. presupuesto duplicado para la misma categoría) |
| `422` | Regla de negocio violada (periodo cerrado, fechas inválidas, sin periodo activo…) |
| `502` | El proveedor de IA del usuario falló (key inválida, caído) |
| `503` | El servidor no tiene `AI_ENCRYPTION_KEY` → `/ai/*` deshabilitado |

La validación corre **dos veces a propósito**: las tags del validator dan `400` por formato y el service
revalida la misma regla dando `422`, para que un CHECK de Postgres nunca llegue al cliente como `500`.

---

## 2. Concepto raíz: el seguimiento (`tracking_period`)

Unidad temporal de 28–31 días, **una activa por usuario**. Toda transacción pertenece a una y **el
backend la asigna solo** — el cliente nunca la elige.

**Regla 8**: cambiar la configuración del seguimiento (duración) aplica al **siguiente** seguimiento,
jamás al activo. `ClosePeriodTx` relee `user_settings` en el momento del cierre.

Varios endpoints hacen **cierre perezoso**: si al resolver el periodo activo resulta que ya terminó, lo
cierran y generan el siguiente antes de responder.

---

## 3. Endpoints

### 3.1 Salud (sin auth)

| Método | Ruta | Notas |
|---|---|---|
| `GET` | `/health` | Liveness, sin dependencias |
| `GET` | `/health/ready` | Readiness, hace ping a la BD |

> Están fuera de `/api/v1`: son `/health` y `/health/ready` a nivel raíz.

### 3.2 Auth — `/auth`

| Método | Ruta | Auth | Notas |
|---|---|---|---|
| `POST` | `/auth/register` | — | Crea user + settings + primer periodo + tokens, atómicamente |
| `POST` | `/auth/login` | — | |
| `POST` | `/auth/refresh` | — | Rota el refresh token |
| `POST` | `/auth/logout` | — | Revoca el refresh token |
| `GET` | `/auth/me` | ✅ | |

⚠️ El registro **no crea una cuenta por defecto**: el usuario nuevo tiene cero `accounts`.

### 3.3 Configuración del usuario — `/settings`

Recurso **singleton** del usuario autenticado (sin id en la ruta).

| Método | Ruta | Notas |
|---|---|---|
| `GET` | `/settings` | |
| `PUT` | `/settings` | **Update parcial**: solo viajan las claves que cambian |

**`PUT` body** (todas opcionales):

```jsonc
{
  "tracking_start_day": 15,      // 1–31
  "tracking_duration_days": 31,  // 28–31
  "default_currency": "COP",     // 3 letras MAYÚSCULAS
  "locale": "es-CO",
  "theme": "system",             // system | light | dark
  "default_period_view": "full"  // full | biweekly | weekly
}
```

Una clave **ausente** deja la columna intacta (`COALESCE(narg, columna)`). Enviar `{}` es un no-op
válido que devuelve el estado actual. `country_code` no es editable y `subscription_tier` lo controla
el servidor.

**Respuesta**:

```jsonc
{
  "tracking_start_day": 15,
  "tracking_duration_days": 31,
  "default_currency": "COP",
  "country_code": "CO",
  "locale": "es-CO",
  "theme": "system",
  "default_period_view": "full",
  "subscription_tier": "free",
  "updated_at": "2026-08-01T02:09:08Z",
  "applies_to_next_period": true,          // siempre true (regla 8)
  "active_period_end_date": "2026-08-30"   // null si no hay periodo activo
}
```

`active_period_end_date` existe para que la app diga la fecha exacta en que el cambio entra en
vigor, en vez de un "aplica después" genérico.

⚠️ `tracking_start_day` **hoy es inerte**: el cierre siempre arranca el siguiente periodo al día
siguiente del anterior y solo estampa este valor como metadata.

### 3.4 Cuentas — `/accounts`

`POST` · `GET` (list) · `GET /:id` · `PUT /:id` · `DELETE /:id`

`current_balance` lo mantiene el backend de forma atómica al crear/editar/borrar transacciones.

### 3.5 Categorías — `/categories`

`POST` · `GET` · `GET /:id` · `PUT /:id` · `DELETE /:id`

20 categorías de sistema (read-only, sembradas en la migración 000012) + las propias del usuario.

### 3.6 Transacciones — `/transactions`

| Método | Ruta | Notas |
|---|---|---|
| `POST` | `/transactions` | |
| `GET` | `/transactions?tracking_period_id=` | Sin el filtro, usa el periodo activo. **Sin paginación** |
| `GET` | `/transactions/:id` | |
| `PUT` | `/transactions/:id` | `tracking_period_id` **no** es editable |
| `DELETE` | `/transactions/:id` | Soft delete |

Tipos: `income`, `expense`, `transfer` (este último requiere `transfer_account_id`).

**Metadata de IA** (opcional, en create y en sync):

```jsonc
{
  "ai_categorized": true,                  // true solo si el usuario CONSERVÓ la sugerencia
  "ai_confidence": "0.87",                 // string decimal
  "ai_suggested_category_id": "uuid"       // lo que sugirió el modelo, aunque el usuario lo cambie
}
```

### 3.7 Presupuestos — `/budgets`

`POST` · `GET` · `GET /:id` · `PUT /:id` · `DELETE /:id`

- Atados al **periodo activo** al crear (con cierre perezoso).
- `category_id` null = presupuesto global del periodo.
- Único por `(periodo, categoría)` → **409**.
- Umbrales de alerta 0–100 (default 80 / 100).
- Los de periodos **cerrados son inmutables** → **422**.
- Hard delete.

### 3.8 Seguimientos — `/tracking-periods` (solo lectura)

| Método | Ruta | Notas |
|---|---|---|
| `GET` | `/tracking-periods` | Lista |
| `GET` | `/tracking-periods/active` | Con cierre perezoso |
| `GET` | `/tracking-periods/:id` | |
| `GET` | `/tracking-periods/:id/summary?view=` | `full` (default) \| `biweekly` \| `weekly` |
| `GET` | `/tracking-periods/:id/insights` | **Polimórfico** (ver abajo) |

**Insights — 16 tipos.** La respuesta depende del estado del periodo:

- **activo** → recalcula perezosamente y devuelve los 7 insights *"during"* (`spending_pace`,
  alertas de presupuesto, gastos hormiga, gasto inusual, `vs_previous_partial`, progreso de metas).
- **cerrado** → devuelve los 9 *"final"*, inmutables, generados al cierre.

Array vacío si aún no hay ninguno. Los "inmediatos" también se refrescan (best-effort) al crear una
transacción.

### 3.9 Metas de ahorro — `/savings-goals`

| Método | Ruta | Notas |
|---|---|---|
| `POST` | `/savings-goals` | → 201 |
| `GET` | `/savings-goals` | Envelope: `{"savings_goals": [...], "count": n}` |
| `GET` | `/savings-goals/:id` | |
| `PUT` | `/savings-goals/:id` | ⚠️ **Replace completo** |
| `DELETE` | `/savings-goals/:id` | → 204 |
| `POST` | `/savings-goals/:id/contributions` | → 201, devuelve `{goal, contribution}` |
| `GET` | `/savings-goals/:id/contributions` | Envelope: `{"contributions": [...], "count": n}` |

⚠️ **El `PUT` es un replace completo**, no un patch: `name`, `target_amount`, `target_date` y `status`
son **requeridos**, y **no acepta `start_date`** (esa fecha es inmutable). Precarga la meta entera en
el formulario antes de editar o borrarás campos.

`status` ∈ `active` | `achieved` | `abandoned` | `paused`.

**Contribuciones**: `CreateContributionTx` actualiza `current_amount` y marca `achieved`
**atómicamente** en el servidor. El cliente nunca lo calcula. Omitir `contribution_date` hace que el
servidor use hoy. Requiere periodo activo → si no hay, **422 `no active tracking period`**.

**Objeto meta**:

```jsonc
{
  "id": "uuid", "name": "Viaje a Cartagena",
  "description": null, "icon": null, "color": null,
  "target_amount": "3000000", "current_amount": "750000", "currency": "COP",
  "start_date": "2026-08-01", "target_date": "2026-12-01",
  "status": "active", "linked_account_id": null,
  "achieved_at": null                       // RFC3339 cuando se logra
}
```

### 3.10 Transacciones recurrentes — `/recurring-transactions`

| Método | Ruta | Notas |
|---|---|---|
| `POST` | `/recurring-transactions` | → 201 |
| `GET` | `/recurring-transactions` | ⚠️ **con efecto secundario** |
| `GET` | `/recurring-transactions/:id` | ⚠️ **con efecto secundario** |
| `PUT` | `/recurring-transactions/:id` | Replace completo; **recalcula `next_due_date`** |
| `DELETE` | `/recurring-transactions/:id` | → 204 |

⚠️ **Los dos `GET` no son lecturas puras**: llaman a `engine.ProcessUserRecurring` antes de responder,
lo que **materializa toda plantilla vencida en transacciones reales** y avanza `next_due_date`. El
cliente debe sincronizar después de llamarlos.

⚠️ **El `PUT` recalcula `next_due_date` desde `start_date`**, así que una edición inocente puede mover
la próxima ejecución. Avísale al usuario.

**Reglas de validación** (`services.validateRecurringInput`, todas → **422**):

| Campo | Regla |
|---|---|
| `transaction_type` | `income` \| `expense` — **`transfer` NO está soportado** |
| `amount` | > 0 |
| `frequency` | `daily` \| `weekly` \| `biweekly` \| `monthly` \| `yearly` \| `custom` |
| `custom_interval_days` | **Obligatorio y > 0** cuando `frequency == "custom"` |
| `day_of_month` | 1–31 |
| `day_of_week` | 0–6, **`EXTRACT(DOW)`: 0 = domingo**. Cuidado: 0 es un valor real, no "sin definir" |
| `end_date` | Estrictamente posterior a `start_date` |

`next_due_date` es **read-only**: lo calcula el motor y queda **`null`** (clave ausente por
`omitempty`) cuando la plantilla se agota; en ese caso llega también `is_active: false`.

### 3.11 Sync offline-first — `/sync`

| Método | Ruta | Notas |
|---|---|---|
| `GET` | `/sync/pull?since=<RFC3339>&page_size=<int>` | Delta por cursor, paginado |
| `POST` | `/sync/push` | Por lotes, idempotente por `client_id` |

**Pull** devuelve las 8 colecciones (`transactions`, `accounts`, `categories`, `budgets`,
`savings_goals`, `goal_contributions`, `recurring_transactions`, `tracking_periods`) más
`server_time` y `has_more`. Incluye soft-deletes (`deleted_at`).

Con `has_more: true`, avanza `since` al `updated_at` más antiguo entre las últimas filas de las
colecciones truncadas. Persiste el cursor **solo** en la última página.

⚠️ **El push solo acepta `entity_type: "transaction"`.** `account`, `category`, `budget`,
`savings_goal`, `contribution` y `recurring` están declarados como constantes pero caen en
`rejected: "unsupported entity_type"`. Asimetría conocida: el pull trae 8 entidades, el push acepta 1.

⚠️ `budgets` y `goal_contributions` hacen **hard delete**, así que desaparecen sin tombstone. No hay
mecanismo de re-sync completo implementado.

### 3.12 IA (BYOK) — `/ai`

| Método | Ruta | Notas |
|---|---|---|
| `PUT` | `/ai/settings` | Guarda proveedor + key (cifrada AES-256-GCM) |
| `GET` | `/ai/settings` | **Nunca devuelve la key**, ni enmascarada |
| `DELETE` | `/ai/settings` | → 204 |
| `POST` | `/ai/categorize` | Sugiere categoría |

**BYOK**: cada usuario trae su propia key. `provider` ∈ `anthropic` | `openai_compatible` (este
último con `base_url`, cubre OpenAI, Groq, OpenRouter, Ollama, LM Studio).

| Código | Significado |
|---|---|
| `503` | El servidor no tiene `AI_ENCRYPTION_KEY` → IA deshabilitada globalmente |
| `422` | El usuario no ha configurado proveedor |
| `502` | La key o el proveedor del usuario fallaron |

`GET /ai/settings` responde `{"configured": false}` cuando no hay nada guardado (no es 404).

`POST /ai/categorize` devuelve `{"category_id": uuid|null, "confidence": 0.0–1.0}`. El servicio
**descarta ids alucinados** que no pertenezcan al usuario y clampa la confianza.

Es **best-effort y online-only**: el cliente debe tragarse los errores y no bloquear la captura.

---

## 4. Huecos conocidos del contrato

- **Sin paginación ni filtros** en ningún listado (`/transactions` devuelve el periodo completo).
- **Sin CORS, rate limiting ni security headers**.
- **Sin OpenAPI/Swagger**.
- `/sync/push` **multi-entidad** sin implementar.
- Multi-moneda es **solo nominal**: hay columna `currency` pero no hay tasas de cambio ni conversión.
- Sin recuperación de contraseña, verificación de email ni borrado de cuenta.
- Sin notificaciones push ni exportación de datos.

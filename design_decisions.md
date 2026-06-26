# Balvia — Decisiones de Diseño de BD (Data Operativa)

> **Versión**: 1.0 inicial
> **Fecha**: 2026-06-24
> **Stack**: PostgreSQL 15+ (Neon), Rust + Axum, Diesel ORM
> **Alcance**: Módulo de data operativa (no incluye Auth detallado)

---

## 1. Filosofía general

### 1.1 El seguimiento (`tracking_period`) es la raíz temporal

Toda la data operativa se organiza alrededor de **seguimientos** de 28-31 días. Esto no es solo un detalle de almacenamiento: es la **unidad mental** con la que el usuario piensa sus finanzas.

**Implicaciones:**
- Las métricas, presupuestos y reportes se calculan SIEMPRE dentro de un seguimiento
- Las transacciones tienen FK obligatoria a un seguimiento
- Los seguimientos cerrados son inmutables (incluyendo sus transacciones)
- Solo puede haber **un seguimiento activo por usuario** (garantizado con `UNIQUE INDEX` parcial)

### 1.2 Separación Auth / Operativa

La tabla `users` aquí es un **placeholder mínimo**. El módulo de Auth la expandirá con `password_hash`, biometría, MFA, etc. Las FKs apuntan a `users.id` y eso es suficiente.

### 1.3 UUIDs en lugar de IDs autoincrementales

**Razones:**
- Sync offline-first: el cliente puede generar IDs sin coordinarse con el servidor
- Sin filtración de cardinalidad (un atacante no sabe cuántos usuarios tienes)
- Mejor para sharding futuro
- Compatible con `client_id` para deduplicación

**Trade-off aceptado:** Mayor tamaño de índice (16 bytes vs 4-8). En la escala de Balvia es irrelevante.

### 1.4 NUMERIC(15,2) para todo lo monetario

Nunca `FLOAT` o `REAL` para dinero. `NUMERIC(15,2)` soporta hasta 9 billones con 2 decimales. Suficiente para Colombia (COP) y cualquier moneda fuerte.

### 1.5 Soft deletes con `deleted_at`

Tablas con borrado lógico: `users`, `accounts`, `categories`, `transactions`, `savings_goals`, `recurring_transactions`.

Tablas con borrado físico: `tracking_periods` (nunca se borran), `budgets`, `summaries`, `insights`, `contributions`.

**Razón:** En las entidades "del usuario" queremos auditoría y posible recuperación. En las entidades "calculadas" no tiene sentido conservar.

---

## 2. Decisiones específicas por tabla

### 2.1 `user_settings`

- **1:1 con users** (no embebido en `users` porque pertenece al módulo operativo, no de auth)
- `tracking_start_day` y `tracking_duration_days` definen cómo se generan los seguimientos
- Cambios en estos campos solo afectan al **siguiente** seguimiento, no a los actuales/pasados

### 2.2 `accounts`

- **NO atadas a seguimiento**: una cuenta existe a lo largo del tiempo
- `current_balance` se mantiene actualizado mediante lógica de aplicación al crear transacciones (no via triggers — más control desde Rust)
- `is_archived` permite "esconder" cuentas sin perder histórico
- `display_order` para que el usuario ordene como quiera

### 2.3 `categories`

- **Mezcla de sistema + usuario**: `is_system = TRUE` y `user_id IS NULL` → categoría global
- **Jerárquicas**: `parent_id` permite subcategorías ("Alimentación → Restaurantes")
- Constraint `chk_system_no_user`: imposible que una categoría sea simultáneamente del sistema Y de un usuario
- Se incluye **seed** con 20 categorías predefinidas pensadas para Colombia

### 2.4 `tracking_periods` — La tabla central

**Garantías a nivel BD (no en aplicación):**
- `chk_duration_range`: duración entre 28 y 31 días
- `uq_one_active_per_user`: máximo un activo por usuario (partial unique index)
- `exclude_overlapping_periods`: dos periodos del mismo usuario NUNCA pueden traslaparse (EXCLUDE constraint con btree_gist)
- `chk_closed_consistency`: si está `closed`, debe tener `closed_at`; si `active`, no

**`sequence_number`**: número correlativo por usuario (1, 2, 3...). Útil para mostrar "Tu seguimiento #12" o navegar histórico.

**`config_start_day` y `config_duration_days`**: snapshot de la config usada al crear ese periodo. Permite saber qué config se usó incluso si el usuario la cambia después.

### 2.5 `transactions` — El corazón

**Validaciones críticas:**
- Trigger `validate_transaction_period`: verifica que (a) el `tracking_period_id` no esté cerrado y (b) `transaction_date` esté dentro del rango del periodo
- `amount > 0` siempre (el "signo" se deriva de `transaction_type`)
- Transferencias requieren `transfer_account_id` distinto al `account_id`

**Por qué `client_id`:** soporte para crear transacciones offline. El cliente genera un ID local; el backend lo respeta para deduplicación (`UNIQUE (user_id, client_id)`).

**Geolocalización opcional**: para insights futuros ("gastas mucho cerca del centro"). Si no se usa, no estorba (NULL).

**IA**: `ai_categorized`, `ai_confidence`, `ai_suggested_category_id` permiten distinguir transacciones categorizadas por IA vs manualmente y rastrear precisión del modelo.

### 2.6 `budgets`

- **Atados a tracking_period**: presupuesto vive y muere con su periodo
- Al cerrar un periodo, el job/handler de cierre **copia los presupuestos al nuevo periodo**
- `category_id NULL` significa "presupuesto global" del periodo (no por categoría)
- `alert_threshold_warning` y `alert_threshold_critical` configurables por presupuesto

### 2.7 `savings_goals` — Metas INDEPENDIENTES

**Por qué no van atadas a seguimientos:**
- Las metas son de **largo plazo** (ahorrar para un carro, vacaciones, etc.)
- Pueden durar 6 meses, 2 años, lo que sea
- El "progreso" se calcula buscando los `savings_goal_contributions` cuyos `tracking_period_id` intersecan con el rango de la meta

**Sugerencias inteligentes**: cuando el usuario consulta una meta, se ejecuta lógica que:
1. Busca el `tracking_period` activo
2. Trae sus `summaries` e `insights` (si están)
3. Calcula "ritmo necesario" vs "ritmo actual"
4. Sugiere recortes en categorías específicas

### 2.8 `recurring_transactions`

- Son **plantillas**, no transacciones reales
- Un job/handler genera transacciones reales al llegar `next_due_date`
- La transacción generada se asocia al `tracking_period` activo en ese momento
- `last_generated_date` y `next_due_date` se actualizan tras cada generación

### 2.9 `tracking_period_summaries`

- **1:1 con tracking_periods cerrados**
- Snapshot inmutable de métricas calculadas
- Columnas JSONB para datos agregados estructurados (gráficos)
- Permite consultar histórico sin recalcular nada

**Estructura típica de `expense_by_category`:**
```json
[
  {"category_id": "uuid", "name": "Alimentación", "amount": 450000, "percentage": 35.2, "transaction_count": 28},
  {"category_id": "uuid", "name": "Transporte", "amount": 280000, "percentage": 21.9, "transaction_count": 12}
]
```

### 2.10 `tracking_period_insights`

**Doble naturaleza:**
- `calculation_phase = 'during'`: insights que se actualizan durante el periodo (recalculables)
- `calculation_phase = 'final'`: insights inmutables generados al cerrar el periodo

**Por qué JSONB en `data`:** cada `insight_type` tiene una estructura propia. Un `ant_expenses` necesita `{count, avg_amount, top_merchant}`, mientras un `budget_warning` necesita `{budget_id, spent, limit, percentage}`. JSONB permite esto sin crear 30 tablas.

**`is_dismissed`**: el usuario puede ocultar insights que ya no le interesan. No se borran (auditoría).

**`valid_from` / `valid_until`**: permite que insights "durante" tengan vigencia limitada (ej: "alerta de presupuesto" se vence cuando se reseteea el periodo).

---

## 3. Flujos clave

### 3.1 Crear transacción (caso normal)

```
1. App envía: { account_id, category_id, amount, type, description }
2. Backend obtiene el tracking_period activo del usuario
3. Backend asigna tracking_period_id + transaction_date = NOW()
4. INSERT en transactions
   → Trigger validate_transaction_period verifica:
       - periodo no cerrado
       - fecha dentro del rango (siempre debe estarlo, pero por seguridad)
5. Backend actualiza accounts.current_balance
6. Backend dispara recálculo de insights "during" rápidos (presupuestos, ritmo)
7. Backend devuelve la transacción creada
```

### 3.2 Cerrar seguimiento

```
1. Job programado (cron en backend Rust) corre cada noche
2. Encuentra tracking_periods con status='active' y end_date <= CURRENT_DATE
3. Para cada uno:
   a. UPDATE status='closed', closed_at=NOW()
   b. Calcula y guarda tracking_period_summaries
   c. Genera insights "final" y los guarda en tracking_period_insights
   d. Crea nuevo tracking_period activo:
      - start_date = end_date_anterior + 1
      - end_date = start_date + (duration_days - 1)
      - Toma config actual del usuario
      - sequence_number = anterior + 1
   e. Copia budgets del anterior al nuevo
   f. Actualiza next_due_date de recurring_transactions que apliquen
4. (Fallback) Al abrir la app, si el periodo del usuario debió cerrarse,
   se ejecuta el flujo de cierre en demanda
```

### 3.3 Calcular insights "durante"

**Disparados por evento (al crear transacción):**
- `budget_warning` / `budget_exceeded`
- `spending_pace` (proyección)

**Calculados al abrir dashboard (lazy):**
- `ant_expenses_early`
- `vs_previous_partial`
- `unusual_expense`
- `goal_progress_alert`

Esto evita jobs pesados y mantiene la app responsiva.

---

## 4. Tipos de insights propuestos

### Durante el seguimiento

| insight_type | severity | Trigger |
|---|---|---|
| `spending_pace` | info/warning | Tras transacción, proyecta gasto total |
| `budget_warning` | warning | Tras transacción, si categoría llega a 80% del presupuesto |
| `budget_exceeded` | critical | Tras transacción, si categoría supera 100% |
| `ant_expenses_early` | warning | Lazy: si en >7 días hay >10 transacciones <$5000 |
| `unusual_expense` | warning | Tras transacción, si supera 3 desv. estándar de su categoría |
| `vs_previous_partial` | info | Lazy: comparación con mismo punto del periodo anterior |
| `goal_progress_alert` | warning | Lazy: si meta va atrasada respecto a fecha objetivo |

### Al cerrar el seguimiento

| insight_type | severity | Descripción |
|---|---|---|
| `top_categories` | info | Top 3 categorías de gasto |
| `top_merchants` | info | Top 3 descripciones/comerciantes |
| `ant_expenses_final` | info/warning | Suma total de gastos hormiga del periodo |
| `reduction_opportunity` | info | "Podrías ahorrar $X bajando Y categoría" |
| `savings_summary` | success/warning | Resumen de ahorro |
| `budget_compliance` | success/warning | Cumplimiento global de presupuestos |
| `vs_previous_final` | info | Comparación completa vs periodo anterior |
| `monthly_wrap_up` | info | "Tu mes en números" (resumen Spotify-Wrapped style) |
| `goal_achievement_summary` | success | Aportes a metas en este periodo |

---

## 5. Consideraciones de performance

### 5.1 Índices clave

- `idx_txn_user_date`: queries de "transacciones del usuario por fecha" (caso más común)
- `idx_txn_tracking_period`: queries de "todas las transacciones de este periodo" (para summaries)
- `idx_txn_category` y `idx_txn_account`: queries de filtrado por categoría/cuenta
- `uq_one_active_per_user`: garantía + lookup rápido del periodo activo
- `idx_insights_user_active`: queries del dashboard de "qué insights mostrar ahora"

### 5.2 Particionamiento (futuro)

Cuando `transactions` crezca a millones de filas (>5M), considerar **particionar por `tracking_period_id`** o por mes. PostgreSQL soporta particiones declarativas. Por ahora innecesario.

### 5.3 Vistas materializadas (futuro)

Para reportes muy pesados (ej: "gasto promedio mensual por categoría en los últimos 12 meses"), considerar **materialized views** que se refrescan al cerrar seguimientos. Por ahora innecesario.

---

## 6. Lo que NO está en este diseño (intencionalmente)

| Feature | Por qué no | Cuándo agregarlo |
|---------|------------|------------------|
| Tags multi-etiquetado | Complejidad innecesaria al inicio | Cuando usuarios lo pidan |
| Adjuntos (recibos) | Requiere storage (S3/Blob) | Sprint posterior |
| Multi-usuario / familiar | Complica el modelo de permisos | Roadmap mid-term |
| Inversiones | Modelo completamente distinto | Tier Premium futuro |
| Integraciones bancarias | Plaid/Belvo es proyecto aparte | Cuando haya tracción |
| Reportes históricos | Se calculan, no se almacenan | Lo manejan los summaries |
| Notificaciones | Tabla aparte cuando se diseñe push | Sprint de notificaciones |
| Auditoría detallada | Solo updated_at por ahora | Si compliance lo requiere |

---

## 7. Próximos pasos

1. **Aplicar migraciones a Neon** (`diesel migration run`)
2. **Generar `schema.rs`** con Diesel CLI (`diesel print-schema > src/schema.rs`)
3. **Crear modelos Rust** en `src/models/` con `#[derive(Queryable, Insertable)]`
4. **Implementar el flujo de Onboarding** (crear user_settings + primer tracking_period)
5. **Implementar el flujo de creación de transacción** (con todas las validaciones)
6. **Implementar el job de cierre de seguimiento**
7. **Diseñar el módulo de Auth** y vincular a `users`

---

## 8. Apéndice — Decisiones abiertas para futuros sprints

Estas decisiones quedaron explícitamente pendientes:

- [ ] **Política de retención de seguimientos antiguos**: ¿se borran después de 3 años? ¿5? ¿nunca?
- [ ] **Estrategia de migración** cuando el usuario cambie de moneda principal
- [ ] **Manejo de inflación** en metas a largo plazo (¿se ajustan automáticamente?)
- [ ] **Backup/restore por usuario** (export/import JSON)
- [ ] **Anonimización** para training del modelo de IA

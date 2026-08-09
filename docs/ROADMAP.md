# Balvia — Estado y roadmap

> Estado real de los dos repos (`Balvia-backend`, `Balvia-movile`).
> Última revisión: **2026-08-08**.
>
> La documentación narrativa vive en el vault de Obsidian (`~/Documents/Balvia Brain/`).
> Este archivo es el resumen operativo que acompaña al código.

---

## ✅ Construido

### Backend (Go 1.26 · Fiber v2 · sqlc · pgx · PostgreSQL/Neon)

- **Auth**: JWT + refresh token rotado (bcrypt, tabla `refresh_tokens`), middleware `JWTAuth`.
- **CRUD**: cuentas, categorías (20 de sistema + propias), transacciones (con `current_balance`
  atómico), presupuestos, metas de ahorro (+ contribuciones), transacciones recurrentes.
- **Seguimientos**: cierre atómico (`ClosePeriodTx`) = cierra + snapshot del summary + genera el
  siguiente + copia presupuestos. Scheduler horario in-process + cierre perezoso.
- **Modo de seguimiento**: `rolling` (bloques de 28-31 días) o `calendar_month` (meses reales),
  elegible por el usuario. El cambio se cubre con un **periodo puente** de 15-45 días
  (`is_transition`), los presupuestos se prorratean al entrar/salir de él y `vs_previous_*` se
  suprime porque compararía periodos de distinta longitud. Aritmética pura en
  `internal/domain/period.go`.
- **Motor de recurrentes**: materialización con catch-up + scheduler horario.
- **Insights**: 16 tipos — 7 *during* (recalculados perezosamente) y 9 *final* (inmutables al cierre).
- **Sync offline-first**: `GET /sync/pull` (delta por cursor, paginado, 8 entidades, soft-deletes) y
  `POST /sync/push` **multi-entidad** (las 7 entidades; contribuciones solo `create`). Por lotes,
  idempotente, con resultado por ítem y detección de conflicto por `updated_at`. Push replica los
  servicios REST, así que una edición offline pasa por las mismas reglas.
- **IA (BYOK)**: categorización automática con key propia del usuario cifrada AES-256-GCM.
- **Configuración de usuario**: `GET/PUT /api/v1/settings`.
- **Onboarding**: el registro provisiona además una cuenta `Efectivo` por defecto, dentro de la
  misma transacción. `PUT /accounts/:id` acepta `initial_balance` mientras la cuenta no tenga
  movimientos (422 si los tiene).
- 18 migraciones aplicadas a Neon. 150 tests (unitarios con mocks + la aritmética de periodos).

### Móvil (Flutter · Riverpod · Drift · Android)

- Auth + splash/redirect, shell de 4 tabs con FAB de captura rápida.
- Captura de gasto en <5s, con sugerencia de categoría por IA.
- CRUD de transacciones, cuentas, categorías, presupuestos.
- **Metas de ahorro** y **transacciones recurrentes** (pantallas completas).
- **Configuración del seguimiento**: modo (mes calendario / ciclo personalizado) y duración. El
  banner nombra las fechas exactas del periodo puente antes de confirmar.
- Carrusel de insights en Inicio.
- **Onboarding de usuario nuevo**: wizard de 3 pasos tras el registro (qué es el seguimiento con
  fechas reales · nombre y saldo inicial de la primera cuenta · dónde está la captura). Omitible.
- Burbuja flotante de sistema (overlay Android).
- 336 tests (unitarios), `flutter analyze` limpio. Drift en `schemaVersion = 3`.

---

## ⏭️ Siguiente

Cerrados el 2026-08-08: **"modo de seguimiento por mes calendario"** y la **mitad de servidor** del
hito de sync multi-entidad. Candidatos, en orden de valor estimado:

1. **Outbox multi-entidad en la app** (mitad restante del hito de sync). El **servidor ya acepta las
   7 entidades** desde el 2026-08-08; falta que la app las encole. Requiere una columna `sync_status`
   en las tablas Drift que aún no la tienen (bump **v3→v4**), extender el outbox del `sync_engine` y
   que los repositorios escriban primero en local. Es lo que permitirá retirar la regla de "todo lo
   que no sea transacción se lee por red".
2. **IA fase 3 — insights narrados / chat**. `POST /ai/assistant` con streaming sobre los 16 insights
   ya calculados, narración tipo "wrapped" encima del carrusel. Reusa toda la infra BYOK. Decisión
   abierta: ¿solo narración o chat completo para el MVP?
3. **IA fase 2 — entrada por voz**. STT on-device (`speech_to_text`) + `POST /ai/parse-expense`.
   Decisión abierta: proveedor de STT. Retos de es-CO ("20 lucas" → 20000).
4. **Deploy y release readiness**. VPS Hetzner + HTTPS, revertir `usesCleartextTraffic`, keystore de
   release real, icono, R8, versionado, ficha de Play Store.

---

## 🐛 Deuda técnica conocida

### Backend

- **Tests faltantes**: `internal/handlers/` (nivel HTTP y `mapDomainError`), `internal/auth/` (JWT +
  bcrypt), `internal/middleware/` (`JWTAuth`), `services/auth.go` (rotación de refresh),
  `services/{category,period}.go`, y `database/store*.go` (toda la lógica transaccional).
  `services/account.go` ya tiene tests, pero solo de la regla del saldo de apertura.
- **Todos los tests son unitarios con mocks**: los triggers de Postgres, el EXCLUDE constraint de
  no-solape, el índice único parcial del periodo activo y los CHECKs nunca se ejercitan.
- **Sin paginación ni filtros** en los listados.
- **Sin CORS, rate limiting ni security headers**. Middleware montado: solo `recover`, `requestid` y
  el logger.
- **Breakdowns JSONB del summary**: faltan `expense_by_day` y `budget_performance`.
- **`vs_previous_*` se suprime en los puentes en vez de normalizarse por día**. Si los puentes
  resultan molestos en la práctica, normalizar es la mejora natural.
- **La aritmética de periodos está duplicada** en Go (`internal/domain/period.go`) y Dart
  (`lib/core/period_math.dart`) para que la app pueda previsualizar el puente sin un endpoint de
  dry-run. Las dos tablas de tests son idénticas a propósito; si la lógica crece, mover al servidor.
- **`tracking_start_day` sigue inerte en modo `rolling`**: el siguiente periodo arranca al día
  siguiente del anterior. Anclarlo a un día del mes exige absorber el desfase en la duración (el
  CHECK 28–31 solo permite ±3 días por ciclo) → hito propio. El **modo calendario ya cubre el caso
  mayoritario** (ancla en el día 1), así que el valor restante de este hito bajó bastante.
- **Multi-moneda solo nominal**: hay columna `currency` pero ni tasas de cambio ni conversión.
- Sin CI, sin Dockerfile, sin deploy.

### Móvil

- **Cero widget tests / integration tests**: los 336 son unitarios. El wizard de onboarding no
  tiene test de UI — solo su controller y el contrato del repositorio de cuentas.
- **Los usuarios registrados antes del 2026-08-03 siguen sin cuenta**: la garantía nueva es del
  registro, no retroactiva, y el wizard solo se le muestra a quien se registra desde ahora. Se
  quedan con el snackbar "Primero crea una cuenta" del FAB.
- **`lib/features/dashboard/dashboard_screen.dart` es código muerto** (532 líneas, no enrutado).
- **Dos capas de lectura conviven**: local (Drift, `sync_providers.dart`) para catálogos y
  transacciones; red (`providers.dart`) para el resto. Ya no hay excusa de servidor — el push acepta
  las 7 entidades desde el 2026-08-08 — así que esto se retira en cuanto el outbox de la app las
  encole.
- **Moneda hardcodeada a COP** en repos antiguos (`AppConfig.defaultCurrency` existe pero el código
  viejo no lo usa).
- **Sin `intl` ni localización**: fechas y montos se formatean a mano, strings es-CO en los widgets.
- **El arranque requiere red**: `AuthController._bootstrap()` llama a `me()` y cualquier excepción
  (incluido "sin internet") manda al login.
- **Sin detección real de conectividad** (`connectivity_plus` no está); el estado offline se infiere
  del último sync.
- **Sync sin disparo periódico**: solo pull-to-refresh, tras mutaciones y por mensaje del overlay.
- Plugin `flutter_overlay_window` vendorizado en `third_party/` vía `dependency_overrides`.

---

## 🚫 Fuera de alcance (decisión, no olvido)

| | Por qué | Cuándo |
|---|---|---|
| iOS | MVP Android-only | Post-MVP |
| Monetización (tiers, IAP, gating) | Sin usuarios que convertir todavía | Con tracción |
| Notificaciones push | Requiere diseño propio | Sprint de notificaciones |
| Tags multi-etiqueta | Complejidad temprana | Cuando lo pidan |
| Adjuntos (recibos) | Requiere storage S3 | Sprint posterior |
| Multiusuario / familiar | Complica permisos | Mid-term |
| Inversiones | Modelo distinto | Tier Premium |
| Integraciones bancarias (Plaid/Belvo) | Proyecto aparte | Con tracción |
| Particionamiento de `transactions` | Solo con >5M filas | Largo plazo |

---

## ❓ Decisiones abiertas

- Retención de seguimientos antiguos: ¿se borran tras 3/5 años? ¿nunca?
- Migración al cambiar de moneda principal.
- Manejo de inflación en metas de largo plazo.
- Backup/restore por usuario (export/import JSON).
- Anonimización y retención para eventual fine-tuning del modelo.
- Presupuesto mensual de costo de IA y gating por tier.
- Proveedor de STT para la entrada por voz.

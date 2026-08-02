# Balvia — Estado y roadmap

> Estado real de los dos repos (`Balvia-backend`, `Balvia-movile`).
> Última revisión: **2026-08-01**.
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
- **Motor de recurrentes**: materialización con catch-up + scheduler horario.
- **Insights**: 16 tipos — 7 *during* (recalculados perezosamente) y 9 *final* (inmutables al cierre).
- **Sync offline-first**: `GET /sync/pull` (delta por cursor, paginado, 8 entidades, soft-deletes) y
  `POST /sync/push` (por lotes, idempotente).
- **IA (BYOK)**: categorización automática con key propia del usuario cifrada AES-256-GCM.
- **Configuración de usuario**: `GET/PUT /api/v1/settings`.
- 17 migraciones aplicadas a Neon. 124 tests (unitarios con mocks).

### Móvil (Flutter · Riverpod · Drift · Android)

- Auth + splash/redirect, shell de 4 tabs con FAB de captura rápida.
- Captura de gasto en <5s, con sugerencia de categoría por IA.
- CRUD de transacciones, cuentas, categorías, presupuestos.
- **Metas de ahorro** y **transacciones recurrentes** (pantallas completas).
- **Configuración del seguimiento** (duración editable).
- Carrusel de insights en Inicio.
- Burbuja flotante de sistema (overlay Android).
- Motor de sync offline-first sobre Drift (`schemaVersion = 2`).
- 304 tests (unitarios), `flutter analyze` limpio.

---

## ⏭️ Siguiente

Sin hito en curso. Candidatos, en orden de valor estimado:

1. **`/sync/push` multi-entidad**. Hoy solo acepta `transaction`; el pull trae 8. Es lo que bloquea
   que cuentas, categorías, presupuestos, metas y recurrentes se puedan mutar sin conexión. Requiere
   además una columna `sync_status` en 3 tablas Drift (bump v2→v3) y extender el outbox.
2. **IA fase 3 — insights narrados / chat**. `POST /ai/assistant` con streaming sobre los 16 insights
   ya calculados, narración tipo "wrapped" encima del carrusel. Reusa toda la infra BYOK. Decisión
   abierta: ¿solo narración o chat completo para el MVP?
3. **IA fase 2 — entrada por voz**. STT on-device (`speech_to_text`) + `POST /ai/parse-expense`.
   Decisión abierta: proveedor de STT. Retos de es-CO ("20 lucas" → 20000).
4. **Deploy y release readiness**. VPS Hetzner + HTTPS, revertir `usesCleartextTraffic`, keystore de
   release real, icono, R8, versionado, ficha de Play Store.
5. **Onboarding de usuario nuevo**. Hoy el registro cae directo en `/home` sin cuenta creada ni
   explicación del concepto de "seguimiento".

---

## 🐛 Deuda técnica conocida

### Backend

- **Tests faltantes**: `internal/handlers/` (nivel HTTP y `mapDomainError`), `internal/auth/` (JWT +
  bcrypt), `internal/middleware/` (`JWTAuth`), `services/auth.go` (rotación de refresh),
  `services/{account,category,period}.go`, y `database/store*.go` (toda la lógica transaccional).
- **Todos los tests son unitarios con mocks**: los triggers de Postgres, el EXCLUDE constraint de
  no-solape, el índice único parcial del periodo activo y los CHECKs nunca se ejercitan.
- **Sin paginación ni filtros** en los listados.
- **Sin CORS, rate limiting ni security headers**. Middleware montado: solo `recover`, `requestid` y
  el logger.
- **Breakdowns JSONB del summary**: faltan `expense_by_day` y `budget_performance`.
- **`tracking_start_day` es inerte**: `ClosePeriodTx` siempre arranca el siguiente periodo al día
  siguiente del anterior. Hacer real el ancla de día del mes exige absorber el desfase en la duración
  (el CHECK 28–31 solo permite ±3 días por ciclo) → hito propio.
- **Multi-moneda solo nominal**: hay columna `currency` pero ni tasas de cambio ni conversión.
- Sin CI, sin Dockerfile, sin deploy.

### Móvil

- **Cero widget tests / integration tests**: los 304 son unitarios.
- **`lib/features/dashboard/dashboard_screen.dart` es código muerto** (532 líneas, no enrutado).
- **Dos capas de lectura conviven**: local (Drift, `sync_providers.dart`) para catálogos y
  transacciones; red (`providers.dart`) para el resto. Es deliberado mientras el push sea de una sola
  entidad, pero confunde.
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

# Balvia — Database Package

Este paquete contiene el diseño completo de la base de datos operativa de Balvia.

## Contenido

| Archivo | Descripción |
|---------|-------------|
| `schema.sql` | DDL completo (todo en un archivo, para referencia rápida) |
| `diagram.md` | Diagrama ER en Mermaid + vista jerárquica de dominios |
| `design_decisions.md` | Documento extenso de decisiones de diseño |
| `migrations/` | Migraciones individuales listas para Diesel CLI |

---

## Cómo aplicar las migraciones con Diesel

### 1. Instalar Diesel CLI (si no lo tienes)

```bash
cargo install diesel_cli --no-default-features --features postgres
```

### 2. Configurar `.env` en tu proyecto Rust

```bash
# .env
DATABASE_URL=postgresql://user:password@ep-xxx.us-east-1.aws.neon.tech/balvia?sslmode=require
```

### 3. Copiar la carpeta de migraciones a tu proyecto

```bash
cp -r migrations /ruta/a/balvia-backend/
cd /ruta/a/balvia-backend/
```

### 4. Ejecutar migraciones

```bash
# Aplicar todas las migraciones
diesel migration run

# Verificar
diesel migration list

# Rollback (si necesitas deshacer la última)
diesel migration revert
```

### 5. Generar el archivo schema.rs de Diesel

```bash
diesel print-schema > src/schema.rs
```

---

## Cómo aplicar el schema directamente (sin Diesel)

Si prefieres usar el `schema.sql` directamente:

```bash
psql "$DATABASE_URL" -f schema.sql
```

---

## Orden de las migraciones

1. **000001_initial_setup** — Extensiones (pgcrypto, btree_gist)
2. **000002_users_placeholder** — Tabla mínima de users
3. **000003_user_settings** — Configuración del usuario
4. **000004_accounts_and_categories** — Cuentas y categorías
5. **000005_tracking_periods** — Seguimientos (raíz temporal)
6. **000006_transactions** — Transacciones (con trigger de validación)
7. **000007_budgets** — Presupuestos por seguimiento
8. **000008_savings_goals** — Metas de ahorro y aportes
9. **000009_recurring_transactions** — Plantillas recurrentes
10. **000010_summaries_and_insights** — Snapshots e insights
11. **000011_triggers_updated_at** — Triggers de auto-update
12. **000012_seed_system_categories** — Categorías predefinidas

---

## Próximos pasos

1. Aplicar migraciones a tu BD en Neon
2. Generar `schema.rs` con `diesel print-schema`
3. Crear estructuras Rust en `src/models/` (Queryable, Insertable)
4. Empezar a implementar el flujo de Onboarding y creación del primer tracking_period
5. Diseñar el módulo de Auth aparte y vincularlo cuando esté listo

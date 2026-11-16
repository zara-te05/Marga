package store

import (
	"context"
	"errors"
	"log"
)

func ptr(s string) *string { return &s }

// Seed crea datos de demostración (idempotente: si existe demo@marga.dev no hace nada).
// hash es el password_hash ya calculado para "marga1234".
func (s *Store) Seed(ctx context.Context, hash string) error {
	if _, err := s.UsuarioPorEmail(ctx, "demo@marga.dev"); err == nil {
		log.Println("seed: los datos demo ya existen, nada que hacer")
		return nil
	} else if !errors.Is(err, ErrNoEncontrado) {
		return err
	}

	eq, err := s.CrearEquipo(ctx, "Equipo Marga")
	if err != nil && !errors.Is(err, ErrConflicto) {
		return err
	}
	var eqID *int64
	if eq.ID != 0 {
		eqID = &eq.ID
	}
	zara, err := s.CrearUsuario(ctx, "zara-te05", "demo@marga.dev", hash, eqID)
	if err != nil {
		return err
	}
	diego, err := s.CrearUsuario(ctx, "diego-z", "diego@marga.dev", hash, eqID)
	if err != nil {
		return err
	}

	fin, err := s.CrearRepo(ctx, zara, DatosRepo{
		Nombre:      ptr("FinTech Core Engine"),
		Descripcion: ptr("Motor transaccional de pagos, ledger contable y conciliación bancaria."),
		Visibilidad: ptr("privado"), Motor: ptr("postgresql"), Entorno: ptr("Producción"),
	})
	if err != nil {
		return err
	}
	if _, err := s.AgregarColaborador(ctx, zara, fin.ID, diego.Email, "escritura"); err != nil {
		return err
	}

	users1 := "CREATE TABLE users (\n    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),\n    email VARCHAR(255) NOT NULL UNIQUE,\n    password_hash TEXT NOT NULL,\n    created_at TIMESTAMPTZ NOT NULL DEFAULT now()\n);"
	users2 := "CREATE TABLE users (\n    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),\n    email VARCHAR(255) NOT NULL UNIQUE,\n    password_hash TEXT NOT NULL,\n    mfa_enabled BOOLEAN NOT NULL DEFAULT false,\n    created_at TIMESTAMPTZ NOT NULL DEFAULT now()\n);"
	wallets := "CREATE TABLE wallets (\n    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),\n    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,\n    currency VARCHAR(3) NOT NULL DEFAULT 'USD',\n    balance NUMERIC(18, 4) NOT NULL DEFAULT 0\n);\nCREATE INDEX idx_wallets_user ON wallets (user_id);"
	trx := "CREATE TABLE transactions (\n    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),\n    wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,\n    amount NUMERIC(18, 4) NOT NULL,\n    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED')),\n    created_at TIMESTAMPTZ NOT NULL DEFAULT now()\n);"
	ledger := "CREATE TABLE ledger_entries (\n    id BIGSERIAL PRIMARY KEY,\n    transaction_id UUID NOT NULL REFERENCES transactions(id),\n    debit NUMERIC(18, 4) NOT NULL DEFAULT 0,\n    credit NUMERIC(18, 4) NOT NULL DEFAULT 0,\n    booked_at TIMESTAMPTZ NOT NULL DEFAULT now()\n);"

	pasos := []struct {
		autor    Usuario
		branch   string
		mensaje  string
		up, down string
		snaps    []NuevoSnapshot
	}{
		{zara, "main", "0001_create_users_table", users1, "DROP TABLE users;", []NuevoSnapshot{{Tabla: "users", DDL: users1}}},
		{zara, "main", "0002_create_wallets_table", wallets, "DROP TABLE wallets;", []NuevoSnapshot{{Tabla: "wallets", DDL: wallets}}},
		{diego, "main", "0003_add_user_security_mfa_column",
			"ALTER TABLE users ADD COLUMN mfa_enabled BOOLEAN NOT NULL DEFAULT false;",
			"ALTER TABLE users DROP COLUMN mfa_enabled;", []NuevoSnapshot{{Tabla: "users", DDL: users2}}},
		{zara, "main", "0004_create_transactions_table", trx, "DROP TABLE transactions;", []NuevoSnapshot{{Tabla: "transactions", DDL: trx}}},
	}
	for _, p := range pasos {
		if _, err := s.CrearCommit(ctx, p.autor, fin.ID, NuevoCommit{
			Branch: p.branch, Mensaje: p.mensaje, SQLUp: p.up, SQLDown: p.down, Snapshots: p.snaps,
		}); err != nil {
			return err
		}
	}
	if _, err := s.CrearBranch(ctx, zara, fin.ID, "feature/ledger", "main"); err != nil {
		return err
	}
	if _, err := s.CrearCommit(ctx, zara, fin.ID, NuevoCommit{
		Branch: "feature/ledger", Mensaje: "0005_create_ledger_entries", SQLUp: ledger,
		SQLDown: "DROP TABLE ledger_entries;", Snapshots: []NuevoSnapshot{{Tabla: "ledger_entries", DDL: ledger}},
	}); err != nil {
		return err
	}

	logi, err := s.CrearRepo(ctx, diego, DatosRepo{
		Nombre:      ptr("Logistics & Fleet API"),
		Descripcion: ptr("Rutas, flotilla y tracking de entregas en tiempo real."),
		Visibilidad: ptr("publico"), Motor: ptr("mysql"), Entorno: ptr("Staging"),
	})
	if err != nil {
		return err
	}
	if _, err := s.CrearCommit(ctx, diego, logi.ID, NuevoCommit{
		Mensaje: "0001_create_vehicles",
		SQLUp:   "CREATE TABLE vehicles (\n    id INT AUTO_INCREMENT PRIMARY KEY,\n    plate VARCHAR(12) NOT NULL UNIQUE\n);",
		SQLDown: "DROP TABLE vehicles;",
	}); err != nil {
		return err
	}

	log.Println("seed: listo → usuario demo@marga.dev / contraseña marga1234")
	return nil
}

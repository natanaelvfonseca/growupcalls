// Ferramenta one-off: migra o device (identidade confiável) do SQLite antigo
// (24/06) para um banco Postgres de sessão, usando a serialização nativa do
// whatsmeow (PutDevice). NÃO toca no SQLite original — opere sobre uma cópia.
//
// Uso (variáveis de ambiente):
//
//	OLD_SQLITE  caminho da CÓPIA do growupcalls.db
//	PG_ADMIN    postgres://user:pass@host:5433/postgres?sslmode=disable
//	SESS_ID     id da sessão (vira o banco growupcalls_<id> e a linha em sessions)
//	SESS_NAME   nome amigável da sessão
package main

import (
	"context"
	"database/sql"
	"log"
	"net/url"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

func replaceDBName(raw, dbName string) string {
	u, err := url.Parse(raw)
	if err != nil {
		log.Fatalf("url inválida %q: %v", raw, err)
	}
	u.Path = "/" + dbName
	return u.String()
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func main() {
	ctx := context.Background()
	wlog := waLog.Stdout("MIG", "INFO", true)

	sqlitePath := os.Getenv("OLD_SQLITE")
	pgAdmin := os.Getenv("PG_ADMIN")
	sessID := os.Getenv("SESS_ID")
	sessName := os.Getenv("SESS_NAME")
	if sqlitePath == "" || pgAdmin == "" || sessID == "" {
		log.Fatal("faltam OLD_SQLITE / PG_ADMIN / SESS_ID")
	}
	if sessName == "" {
		sessName = "WhatsApp"
	}

	// 1) abre a CÓPIA do SQLite antigo e sobe o schema p/ ler o device.
	// O driver do modernc se chama "sqlite"; o dialeto do whatsmeow é "sqlite3".
	oldDSN := "file:" + sqlitePath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"
	oldDB, err := sql.Open("sqlite", oldDSN)
	if err != nil {
		log.Fatal("abrir sqlite:", err)
	}
	oldDB.SetMaxOpenConns(1)
	oldC := sqlstore.NewWithDB(oldDB, "sqlite3", wlog)
	if err := oldC.Upgrade(ctx); err != nil {
		log.Fatal("upgrade sqlite:", err)
	}
	dev, err := oldC.GetFirstDevice(ctx)
	if err != nil {
		log.Fatal("ler device:", err)
	}
	if dev.ID == nil {
		log.Fatal("nenhum device pareado no sqlite antigo")
	}
	log.Printf("device antigo: jid=%s lid=%s reg=%d push=%q", dev.ID, dev.LID, dev.RegistrationID, dev.PushName)

	// 2) cria o banco growupcalls_<id> no Postgres (ignora se já existe)
	admin, err := sql.Open("pgx", pgAdmin)
	if err != nil {
		log.Fatal("abrir pg admin:", err)
	}
	dbName := "growupcalls_" + sessID
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+quoteIdent(dbName)); err != nil {
		log.Printf("CREATE DATABASE %s: %v (ok se já existir)", dbName, err)
	}

	// 3) abre o container Postgres da sessão, sobe o schema e grava o device
	sessDB, err := sql.Open("pgx", replaceDBName(pgAdmin, dbName))
	if err != nil {
		log.Fatal("abrir pg sessão:", err)
	}
	newC := sqlstore.NewWithDB(sessDB, "postgres", wlog)
	if err := newC.Upgrade(ctx); err != nil {
		log.Fatal("upgrade pg sessão:", err)
	}
	if err := newC.PutDevice(ctx, dev); err != nil {
		log.Fatal("gravar device no postgres:", err)
	}
	log.Printf("device gravado em %s", dbName)

	// 4) registra a linha de config no banco principal (growupcalls_main)
	mdb, err := sql.Open("pgx", replaceDBName(pgAdmin, "growupcalls_main"))
	if err != nil {
		log.Fatal("abrir growupcalls_main:", err)
	}
	if _, err := mdb.ExecContext(ctx,
		`INSERT INTO sessions (id, name, jid) VALUES ($1,$2,$3)
		 ON CONFLICT (id) DO UPDATE SET jid=excluded.jid, name=excluded.name`,
		sessID, sessName, dev.ID.String()); err != nil {
		log.Fatal("inserir config em growupcalls_main:", err)
	}
	log.Println("OK: identidade de 24/06 migrada para o Postgres. Reinicie o app.")
}

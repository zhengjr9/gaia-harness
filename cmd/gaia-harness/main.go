package main

import (
	"database/sql"
	"flag"
	"log"
	"net/http"

	"github.com/zhengjiarui/gaia-harness/httpapi"
	"github.com/zhengjiarui/gaia-harness/session"
	_ "modernc.org/sqlite"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "gaia-harness.db", "sqlite database path")
	flag.Parse()
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	store, err := session.NewSQLiteStore(db)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("gaia-harness listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, (httpapi.Server{Sessions: session.Service{Store: store}}).Handler()))
}

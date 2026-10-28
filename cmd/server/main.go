package main

import (
	"context"
	"log"
	"net/http"

	"github.com/guide-flow/guide-flow/internal/bootstrap"
)

func main() {
	app, err := bootstrap.Build(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	log.Printf("guide-flow API listening on %s", app.Config.Server.HTTPAddr)
	log.Fatal(http.ListenAndServe(app.Config.Server.HTTPAddr, app.Handler))
}

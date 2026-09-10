package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/devtools-asset-upload/internal/assets"
	"example.com/devtools-asset-upload/internal/infrai"
)

const bucketName = "developer-tool-assets"

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := infrai.New(apiKey)

	broker := assets.NewBroker(bucketName, assets.NewInfraiSigner(client))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload-intents", uploadIntentHandler(broker))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("asset upload broker listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func uploadIntentHandler(broker *assets.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input assets.UploadRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"accepted": false, "diagnostic": assets.Diagnostic{Code: "invalid_request", Message: "request body must match the upload intent schema"}})
			return
		}
		decision, err := broker.Decide(r.Context(), input)
		if err != nil {
			var apiErr *infrai.APIError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
				writeJSON(w, apiErr.Status, map[string]any{"accepted": false, "diagnostic": assets.Diagnostic{Code: apiErr.Code, Message: apiErr.Message}})
				return
			}
			log.Printf("sign upload: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"accepted": false, "diagnostic": assets.Diagnostic{Code: "signing_unavailable", Message: "upload intent could not be issued"}})
			return
		}
		status := http.StatusOK
		if !decision.Accepted {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, decision)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

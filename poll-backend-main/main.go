package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Option struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
}

type PollResponse struct {
	Question string   `json:"question"`
	Options  []Option `json:"options"`
}

type VoteRequest struct {
	OptionID string `json:"optionId"`
}

var (
	ddbClient     *dynamodb.Client
	tableName     string
	pollQuestion  string
	allowedOrigin string
)

func main() {
	tableName = getEnv("DYNAMODB_TABLE", "PollOptions")
	pollQuestion = getEnv("POLL_QUESTION", "¿Cuál es tu lenguaje de programación favorito?")
	allowedOrigin = getEnv("CORS_ALLOWED_ORIGIN", "*")

	cfg, err := config.LoadDefaultConfig(context.TODO())

	if err != nil {
		log.Fatalf("no se pudo cargar la configuración de AWS: %v", err)
	}
	ddbClient = dynamodb.NewFromConfig(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/poll", withCORS(getPollHandler))
	mux.HandleFunc("POST /api/poll/vote", withCORS(voteHandler))
	mux.HandleFunc("POST /api/poll/reset", withCORS(resetHandler))
	mux.HandleFunc("OPTIONS /api/poll", withCORS(noop))
	mux.HandleFunc("OPTIONS /api/poll/vote", withCORS(noop))
	mux.HandleFunc("OPTIONS /api/poll/reset", withCORS(noop))

	log.Println("servidor escuchando en :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

func noop(w http.ResponseWriter, r *http.Request) {}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// withCORS agrega los headers necesarios para que el frontend
// (servido desde otro dominio en Amplify) pueda llamar a esta API.
func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func fetchPoll(ctx context.Context) (PollResponse, error) {
	out, err := ddbClient.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return PollResponse{}, err
	}

	options := make([]Option, 0, len(out.Items))
	for _, item := range out.Items {
		id, _ := item["OptionID"].(*types.AttributeValueMemberS)
		text, _ := item["OptionText"].(*types.AttributeValueMemberS)
		votesAttr, _ := item["Votes"].(*types.AttributeValueMemberN)

		votes := 0
		if votesAttr != nil {
			votes, _ = strconv.Atoi(votesAttr.Value)
		}

		options = append(options, Option{
			ID:    stringValue(id),
			Text:  stringValue(text),
			Votes: votes,
		})
	}

	return PollResponse{Question: pollQuestion, Options: options}, nil
}

func stringValue(s *types.AttributeValueMemberS) string {
	if s == nil {
		return ""
	}
	return s.Value
}

func getPollHandler(w http.ResponseWriter, r *http.Request) {
	poll, err := fetchPoll(r.Context())
	if err != nil {
		http.Error(w, "error al leer la encuesta", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, poll)
}

func voteHandler(w http.ResponseWriter, r *http.Request) {
	var req VoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OptionID == "" {
		http.Error(w, `cuerpo inválido, se espera {"optionId": "..."}`, http.StatusBadRequest)
		return
	}

	_, err := ddbClient.UpdateItem(r.Context(), &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"OptionID": &types.AttributeValueMemberS{Value: req.OptionID},
		},
		UpdateExpression:    aws.String("ADD Votes :inc"),
		ConditionExpression: aws.String("attribute_exists(OptionID)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":inc": &types.AttributeValueMemberN{Value: "1"},
		},
	})
	if err != nil {
		http.Error(w, "no se pudo registrar el voto (¿la opción existe?)", http.StatusBadRequest)
		return
	}

	poll, err := fetchPoll(r.Context())
	if err != nil {
		http.Error(w, "voto registrado, pero no se pudo recargar la encuesta", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, poll)
}

func resetHandler(w http.ResponseWriter, r *http.Request) {
	poll, err := fetchPoll(r.Context())
	if err != nil {
		http.Error(w, "error al leer la encuesta", http.StatusInternalServerError)
		return
	}

	for _, opt := range poll.Options {
		_, err := ddbClient.UpdateItem(r.Context(), &dynamodb.UpdateItemInput{
			TableName: aws.String(tableName),
			Key: map[string]types.AttributeValue{
				"OptionID": &types.AttributeValueMemberS{Value: opt.ID},
			},
			UpdateExpression: aws.String("SET Votes = :zero"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":zero": &types.AttributeValueMemberN{Value: "0"},
			},
		})
		if err != nil {
			http.Error(w, "error al reiniciar la encuesta", http.StatusInternalServerError)
			return
		}
	}

	updated, _ := fetchPoll(r.Context())
	writeJSON(w, http.StatusOK, updated)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

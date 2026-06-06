// Command ws is the WebSocket Lambda backing the Gotify /stream endpoint. It
// authorizes the $connect handshake with a client token, records the live
// connection in DynamoDB (so the api Lambda can fan messages out to it), and
// cleans up on $disconnect.
package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/mjlocat/serverless-notify/internal/store"
)

type handler struct {
	store *store.Store
}

func main() {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("load aws config: %v", err)
	}
	h := &handler{store: store.New(
		dynamodb.NewFromConfig(cfg),
		os.Getenv("TABLE_NAME"),
		os.Getenv("BYTOKEN_INDEX"),
		os.Getenv("BYAPP_INDEX"),
		0,
	)}
	lambda.Start(h.handle)
}

func (h *handler) handle(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connID := req.RequestContext.ConnectionID
	switch req.RequestContext.RouteKey {
	case "$connect":
		token := req.QueryStringParameters["token"]
		if token == "" || token[0] != 'C' {
			return events.APIGatewayProxyResponse{StatusCode: 401}, nil
		}
		client, ok, err := h.store.GetClientByToken(ctx, token)
		if err != nil {
			log.Printf("$connect: token lookup: %v", err)
			return events.APIGatewayProxyResponse{StatusCode: 500}, nil
		}
		if !ok {
			return events.APIGatewayProxyResponse{StatusCode: 401}, nil
		}
		if err := h.store.SaveConnection(ctx, connID, client.ID); err != nil {
			log.Printf("$connect: save connection: %v", err)
			return events.APIGatewayProxyResponse{StatusCode: 500}, nil
		}
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil

	case "$disconnect":
		if err := h.store.DeleteConnection(ctx, connID); err != nil {
			log.Printf("$disconnect: %v", err)
		}
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil

	default:
		// The Gotify app does not send frames; accept and ignore keep-alives.
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil
	}
}

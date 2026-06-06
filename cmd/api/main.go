// Command api is the HTTP API Lambda: a single Go binary behind API Gateway's
// $default route that implements the Gotify-compatible REST surface (senders,
// applications, clients, messages) and fans new messages out to live WebSocket
// connections.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"mime/multipart"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	apitypes "github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/mjlocat/serverless-notify/internal/auth"
	"github.com/mjlocat/serverless-notify/internal/gotify"
	"github.com/mjlocat/serverless-notify/internal/store"
)

const (
	defaultLimit = 100
	maxLimit     = 200
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type server struct {
	store   *store.Store
	wsAPI   *apigatewaymanagementapi.Client
	creds   credentials
	baseURL string
}

func main() {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load aws config: %v", err)
	}

	ttl := time.Duration(0)
	if days, _ := strconv.Atoi(os.Getenv("MESSAGE_TTL_DAYS")); days > 0 {
		ttl = time.Duration(days) * 24 * time.Hour
	}

	st := store.New(
		dynamodb.NewFromConfig(cfg),
		os.Getenv("TABLE_NAME"),
		os.Getenv("BYTOKEN_INDEX"),
		os.Getenv("BYAPP_INDEX"),
		ttl,
	)

	wsEndpoint := os.Getenv("WS_MGMT_ENDPOINT")
	wsAPI := apigatewaymanagementapi.NewFromConfig(cfg, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = aws.String(wsEndpoint)
	})

	creds, err := loadCredentials(ctx, cfg, os.Getenv("SSM_AUTH_PARAM"))
	if err != nil {
		log.Fatalf("load credentials: %v", err)
	}

	s := &server{store: st, wsAPI: wsAPI, creds: creds, baseURL: strings.TrimRight(os.Getenv("BASE_URL"), "/")}
	lambda.Start(s.handle)
}

// loadCredentials fetches the single-user basic-auth credentials from SSM once
// at cold start.
func loadCredentials(ctx context.Context, cfg aws.Config, param string) (credentials, error) {
	out, err := ssm.NewFromConfig(cfg).GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &param,
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return credentials{}, err
	}
	var c credentials
	return c, json.Unmarshal([]byte(*out.Parameter.Value), &c)
}

func (s *server) handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	method := req.RequestContext.HTTP.Method
	path := "/" + strings.Trim(req.RawPath, "/")
	if path == "/" {
		path = req.RawPath
	}
	segs := splitPath(req.RawPath)

	if method == "OPTIONS" {
		return resp(204, nil), nil
	}

	switch {
	case method == "GET" && path == "/health":
		return s.json(200, gotify.Health{Health: "green", Database: "green"}), nil
	case method == "GET" && path == "/version":
		return s.json(200, gotify.VersionInfo{Version: "2.6.3", Commit: "serverless-notify", BuildDate: "1970-01-01T00:00:00Z"}), nil
	case method == "GET" && path == "/current/user":
		if !s.authClient(ctx, req) {
			return s.unauthorized(), nil
		}
		return s.json(200, gotify.User{ID: 1, Name: s.creds.Username, Admin: true}), nil

	// --- messages ---
	case method == "POST" && path == "/message":
		return s.postMessage(ctx, req)
	case method == "GET" && path == "/message":
		return s.getMessages(ctx, req)
	case method == "DELETE" && path == "/message":
		return s.deleteAllMessages(ctx, req)
	case method == "DELETE" && len(segs) == 2 && segs[0] == "message":
		return s.deleteMessage(ctx, req, segs[1])

	// --- applications ---
	case method == "GET" && path == "/application":
		return s.listApplications(ctx, req)
	case method == "POST" && path == "/application":
		return s.createApplication(ctx, req)
	case method == "PUT" && len(segs) == 2 && segs[0] == "application":
		return s.updateApplication(ctx, req, segs[1])
	case method == "DELETE" && len(segs) == 2 && segs[0] == "application":
		return s.deleteApplication(ctx, req, segs[1])
	case method == "GET" && len(segs) == 3 && segs[0] == "application" && segs[2] == "message":
		return s.getAppMessages(ctx, req, segs[1])
	case method == "DELETE" && len(segs) == 3 && segs[0] == "application" && segs[2] == "message":
		return s.deleteAppMessages(ctx, req, segs[1])
	case method == "POST" && len(segs) == 3 && segs[0] == "application" && segs[2] == "image":
		return s.uploadAppImage(ctx, req, segs[1])

	// --- clients ---
	case method == "GET" && path == "/client":
		return s.listClients(ctx, req)
	case method == "POST" && path == "/client":
		return s.createClient(ctx, req)
	case method == "PUT" && len(segs) == 2 && segs[0] == "client":
		return s.updateClient(ctx, req, segs[1])
	case method == "DELETE" && len(segs) == 2 && segs[0] == "client":
		return s.deleteClient(ctx, req, segs[1])
	}
	return s.error(404, "Not Found", "page not found"), nil
}

// --- message handlers ----------------------------------------------------

func (s *server) postMessage(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	appID, ok := s.authApp(ctx, req)
	if !ok {
		return s.unauthorized(), nil
	}
	title, message, priority, hasPriority, extras, err := parseMessageBody(req)
	if err != nil {
		return s.error(400, "Bad Request", err.Error()), nil
	}
	if message == "" {
		return s.error(400, "Bad Request", "message is required"), nil
	}
	if !hasPriority {
		if app, found, _ := s.store.GetApplication(ctx, appID); found {
			priority = app.DefaultPriority
		}
	}
	id, err := s.store.NextID(ctx, "message")
	if err != nil {
		return s.serverError(err), nil
	}
	msg := gotify.Message{
		ID:            id,
		ApplicationID: appID,
		Message:       message,
		Title:         title,
		Priority:      priority,
		Date:          time.Now().UTC().Format(time.RFC3339Nano),
		Extras:        extras,
	}
	if err := s.store.PutMessage(ctx, msg); err != nil {
		return s.serverError(err), nil
	}
	s.broadcast(ctx, msg)
	return s.json(200, msg), nil
}

func (s *server) getMessages(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	since, limit := paging(req.QueryStringParameters)
	msgs, err := s.store.ListMessages(ctx, since, limit)
	if err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, s.page(msgs, limit, "/message", 0)), nil
}

func (s *server) getAppMessages(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	appID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid application id"), nil
	}
	since, limit := paging(req.QueryStringParameters)
	msgs, err := s.store.ListAppMessages(ctx, appID, since, limit)
	if err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, s.page(msgs, limit, "/application/"+idStr+"/message", appID)), nil
}

func (s *server) deleteMessage(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid message id"), nil
	}
	if err := s.store.DeleteMessage(ctx, id); err != nil {
		return s.serverError(err), nil
	}
	return resp(200, nil), nil
}

func (s *server) deleteAllMessages(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	if err := s.store.DeleteAllMessages(ctx); err != nil {
		return s.serverError(err), nil
	}
	return resp(200, nil), nil
}

func (s *server) deleteAppMessages(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	appID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid application id"), nil
	}
	if err := s.store.DeleteAppMessages(ctx, appID); err != nil {
		return s.serverError(err), nil
	}
	return resp(200, nil), nil
}

// --- application handlers ------------------------------------------------

func (s *server) listApplications(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	apps, err := s.store.ListApplications(ctx)
	if err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, apps), nil
}

func (s *server) createApplication(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		DefaultPriority int    `json:"defaultPriority"`
	}
	if err := decodeJSON(req, &body); err != nil {
		return s.error(400, "Bad Request", err.Error()), nil
	}
	id, err := s.store.NextID(ctx, "application")
	if err != nil {
		return s.serverError(err), nil
	}
	app := gotify.Application{
		ID:              id,
		Token:           gotify.GenerateApplicationToken(),
		Name:            body.Name,
		Description:     body.Description,
		DefaultPriority: body.DefaultPriority,
		Image:           "static/defaultapp.png",
	}
	if err := s.store.PutApplication(ctx, app); err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, app), nil
}

func (s *server) updateApplication(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid application id"), nil
	}
	app, found, err := s.store.GetApplication(ctx, id)
	if err != nil {
		return s.serverError(err), nil
	}
	if !found {
		return s.error(404, "Not Found", "application does not exist"), nil
	}
	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		DefaultPriority int    `json:"defaultPriority"`
	}
	if err := decodeJSON(req, &body); err != nil {
		return s.error(400, "Bad Request", err.Error()), nil
	}
	app.Name, app.Description, app.DefaultPriority = body.Name, body.Description, body.DefaultPriority
	if err := s.store.PutApplication(ctx, app); err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, app), nil
}

func (s *server) deleteApplication(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid application id"), nil
	}
	if err := s.store.DeleteApplication(ctx, id); err != nil {
		return s.serverError(err), nil
	}
	return resp(200, nil), nil
}

// uploadAppImage is a minimal stub: app icons are optional for the Android app
// (it falls back to a placeholder). It acknowledges the upload without storing.
func (s *server) uploadAppImage(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid application id"), nil
	}
	app, found, err := s.store.GetApplication(ctx, id)
	if err != nil {
		return s.serverError(err), nil
	}
	if !found {
		return s.error(404, "Not Found", "application does not exist"), nil
	}
	return s.json(200, app), nil
}

// --- client handlers -----------------------------------------------------

func (s *server) listClients(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	clients, err := s.store.ListClients(ctx)
	if err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, clients), nil
}

func (s *server) createClient(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(req, &body); err != nil {
		return s.error(400, "Bad Request", err.Error()), nil
	}
	id, err := s.store.NextID(ctx, "client")
	if err != nil {
		return s.serverError(err), nil
	}
	c := gotify.Client{ID: id, Token: gotify.GenerateClientToken(), Name: body.Name}
	if err := s.store.PutClient(ctx, c); err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, c), nil
}

func (s *server) updateClient(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid client id"), nil
	}
	c, found, err := s.store.GetClient(ctx, id)
	if err != nil {
		return s.serverError(err), nil
	}
	if !found {
		return s.error(404, "Not Found", "client does not exist"), nil
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(req, &body); err != nil {
		return s.error(400, "Bad Request", err.Error()), nil
	}
	c.Name = body.Name
	if err := s.store.PutClient(ctx, c); err != nil {
		return s.serverError(err), nil
	}
	return s.json(200, c), nil
}

func (s *server) deleteClient(ctx context.Context, req events.APIGatewayV2HTTPRequest, idStr string) (events.APIGatewayV2HTTPResponse, error) {
	if !s.authClient(ctx, req) {
		return s.unauthorized(), nil
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return s.error(400, "Bad Request", "invalid client id"), nil
	}
	if err := s.store.DeleteClient(ctx, id); err != nil {
		return s.serverError(err), nil
	}
	return resp(200, nil), nil
}

// --- delivery ------------------------------------------------------------

// broadcast pushes a message to every live WebSocket connection, pruning stale
// ones. Delivery failures are logged, not fatal: the app re-fetches missed
// messages via GET /message on reconnect.
func (s *server) broadcast(ctx context.Context, msg gotify.Message) {
	conns, err := s.store.ListConnections(ctx)
	if err != nil {
		log.Printf("broadcast: list connections: %v", err)
		return
	}
	if len(conns) == 0 {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("broadcast: marshal: %v", err)
		return
	}
	for _, connID := range conns {
		_, err := s.wsAPI.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
			ConnectionId: aws.String(connID),
			Data:         data,
		})
		if err == nil {
			continue
		}
		var gone *apitypes.GoneException
		if errors.As(err, &gone) {
			if delErr := s.store.DeleteConnection(ctx, connID); delErr != nil {
				log.Printf("broadcast: prune %s: %v", connID, delErr)
			}
			continue
		}
		log.Printf("broadcast: post to %s: %v", connID, err)
	}
}

// --- auth ----------------------------------------------------------------

// authApp authorizes a sender via an application token and returns its app id.
func (s *server) authApp(ctx context.Context, req events.APIGatewayV2HTTPRequest) (int64, bool) {
	token := auth.ExtractToken(req.Headers, req.QueryStringParameters)
	if token == "" || token[0] != 'A' {
		return 0, false
	}
	app, ok, err := s.store.GetApplicationByToken(ctx, token)
	if err != nil {
		log.Printf("authApp: %v", err)
		return 0, false
	}
	return app.ID, ok
}

// authClient authorizes a management/receiver request via a client token or the
// single-user basic-auth credentials.
func (s *server) authClient(ctx context.Context, req events.APIGatewayV2HTTPRequest) bool {
	if token := auth.ExtractToken(req.Headers, req.QueryStringParameters); token != "" && token[0] == 'C' {
		_, ok, err := s.store.GetClientByToken(ctx, token)
		if err != nil {
			log.Printf("authClient: %v", err)
			return false
		}
		if ok {
			return true
		}
	}
	if user, pass, ok := auth.BasicAuth(req.Headers); ok {
		return auth.Equal(user, s.creds.Username) && auth.Equal(pass, s.creds.Password)
	}
	return false
}

// --- helpers -------------------------------------------------------------

func (s *server) page(msgs []gotify.Message, limit int32, path string, appID int64) gotify.PagedMessages {
	p := gotify.Paging{Size: len(msgs), Limit: int(limit)}
	if len(msgs) == int(limit) && len(msgs) > 0 {
		last := msgs[len(msgs)-1].ID
		p.Since = last
		p.Next = s.baseURL + path + "?limit=" + strconv.Itoa(int(limit)) + "&since=" + strconv.FormatInt(last, 10)
	}
	return gotify.PagedMessages{Messages: msgs, Paging: p}
}

// paging parses since/limit query parameters with Gotify's defaults and cap.
func paging(q map[string]string) (since int64, limit int32) {
	since, _ = strconv.ParseInt(q["since"], 10, 64)
	l, err := strconv.Atoi(q["limit"])
	if err != nil || l <= 0 {
		l = defaultLimit
	}
	if l > maxLimit {
		l = maxLimit
	}
	return since, int32(l)
}

// parseMessageBody supports the three content types Gotify accepts for
// POST /message: application/json, multipart/form-data and
// application/x-www-form-urlencoded.
func parseMessageBody(req events.APIGatewayV2HTTPRequest) (title, message string, priority int, hasPriority bool, extras map[string]interface{}, err error) {
	body := decodeBody(req)
	ct := req.Headers["content-type"]

	switch {
	case strings.HasPrefix(ct, "application/json"):
		var b struct {
			Title    string                 `json:"title"`
			Message  string                 `json:"message"`
			Priority *int                   `json:"priority"`
			Extras   map[string]interface{} `json:"extras"`
		}
		if err = json.Unmarshal([]byte(body), &b); err != nil {
			return
		}
		title, message, extras = b.Title, b.Message, b.Extras
		if b.Priority != nil {
			priority, hasPriority = *b.Priority, true
		}
		return

	case strings.HasPrefix(ct, "multipart/form-data"):
		_, params, perr := mime.ParseMediaType(ct)
		if perr != nil {
			err = perr
			return
		}
		form, ferr := multipart.NewReader(strings.NewReader(body), params["boundary"]).ReadForm(8 << 20)
		if ferr != nil {
			err = ferr
			return
		}
		defer form.RemoveAll()
		title, message = firstValue(form.Value["title"]), firstValue(form.Value["message"])
		if p := firstValue(form.Value["priority"]); p != "" {
			if pv, cerr := strconv.Atoi(p); cerr == nil {
				priority, hasPriority = pv, true
			}
		}
		return

	default:
		// application/x-www-form-urlencoded (the common Gotify client default).
		vals, perr := url.ParseQuery(body)
		if perr != nil {
			err = perr
			return
		}
		title, message = vals.Get("title"), vals.Get("message")
		if p := vals.Get("priority"); p != "" {
			if pv, cerr := strconv.Atoi(p); cerr == nil {
				priority, hasPriority = pv, true
			}
		}
		return
	}
}

func firstValue(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func decodeJSON(req events.APIGatewayV2HTTPRequest, v interface{}) error {
	body := decodeBody(req)
	if strings.TrimSpace(body) == "" {
		return nil
	}
	return json.Unmarshal([]byte(body), v)
}

func decodeBody(req events.APIGatewayV2HTTPRequest) string {
	if req.IsBase64Encoded {
		if raw, err := base64.StdEncoding.DecodeString(req.Body); err == nil {
			return string(raw)
		}
	}
	return req.Body
}

func splitPath(raw string) []string {
	trimmed := strings.Trim(raw, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// --- responses -----------------------------------------------------------

func corsHeaders(contentType string) map[string]string {
	h := map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
		"Access-Control-Allow-Headers": "Authorization, Content-Type, X-Gotify-Key",
	}
	if contentType != "" {
		h["Content-Type"] = contentType
	}
	return h
}

func resp(status int, body []byte) events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{StatusCode: status, Headers: corsHeaders(""), Body: string(body)}
}

func (s *server) json(status int, v interface{}) events.APIGatewayV2HTTPResponse {
	b, err := json.Marshal(v)
	if err != nil {
		return s.serverError(err)
	}
	return events.APIGatewayV2HTTPResponse{StatusCode: status, Headers: corsHeaders("application/json"), Body: string(b)}
}

func (s *server) error(status int, errStr, desc string) events.APIGatewayV2HTTPResponse {
	return s.json(status, gotify.Error{Error: errStr, ErrorCode: status, ErrorDescription: desc})
}

func (s *server) unauthorized() events.APIGatewayV2HTTPResponse {
	return s.error(401, "Unauthorized", "you need to provide a valid access token or user credentials to access this api")
}

func (s *server) serverError(err error) events.APIGatewayV2HTTPResponse {
	log.Printf("internal error: %v", err)
	return s.error(500, "Internal Server Error", "something went wrong")
}

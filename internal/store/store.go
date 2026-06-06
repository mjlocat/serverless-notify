// Package store is the DynamoDB access layer. It uses a single table with a
// few item collections (applications, clients, messages, connections, an id
// counter) and two GSIs:
//
//   - byToken (HASH tokenPK): resolve an app/client token on every auth check.
//   - byApp   (HASH appPK, RANGE appSK): list/delete a single app's messages.
//
// Partition layout (base table):
//
//	Application  PK="APP"        SK=<padded id>
//	Client       PK="CLIENT"     SK=<padded id>
//	Message      PK="MSG"        SK=<padded id>
//	Connection   PK="CONN"       SK=<connectionId>
//	Counter      PK="COUNTER"    SK=<name>
package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/mjlocat/serverless-notify/internal/gotify"
)

// Store wraps a DynamoDB client and the table/index names plus optional
// message TTL.
type Store struct {
	db         *dynamodb.Client
	table      string
	byToken    string
	byApp      string
	messageTTL time.Duration
}

// New constructs a Store.
func New(db *dynamodb.Client, table, byToken, byApp string, messageTTL time.Duration) *Store {
	return &Store{db: db, table: table, byToken: byToken, byApp: byApp, messageTTL: messageTTL}
}

// --- key helpers ---------------------------------------------------------

func padID(id int64) string { return fmt.Sprintf("%020d", id) }

func s(v string) ddbtypes.AttributeValue {
	return &ddbtypes.AttributeValueMemberS{Value: v}
}
func num(v int64) ddbtypes.AttributeValue {
	return &ddbtypes.AttributeValueMemberN{Value: strconv.FormatInt(v, 10)}
}

func appKey(id int64) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{"PK": s("APP"), "SK": s(padID(id))}
}
func clientKey(id int64) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{"PK": s("CLIENT"), "SK": s(padID(id))}
}
func msgKey(id int64) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{"PK": s("MSG"), "SK": s(padID(id))}
}
func connKey(connID string) map[string]ddbtypes.AttributeValue {
	return map[string]ddbtypes.AttributeValue{"PK": s("CONN"), "SK": s(connID)}
}

// --- counters ------------------------------------------------------------

// NextID atomically increments and returns the named counter, giving the
// monotonic integer ids Gotify's paging relies on.
func (st *Store) NextID(ctx context.Context, name string) (int64, error) {
	out, err := st.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &st.table,
		Key: map[string]ddbtypes.AttributeValue{
			"PK": s("COUNTER"), "SK": s(name),
		},
		UpdateExpression:          aws.String("ADD #v :one"),
		ExpressionAttributeNames:  map[string]string{"#v": "value"},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":one": num(1)},
		ReturnValues:              ddbtypes.ReturnValueUpdatedNew,
	})
	if err != nil {
		return 0, err
	}
	v, ok := out.Attributes["value"].(*ddbtypes.AttributeValueMemberN)
	if !ok {
		return 0, fmt.Errorf("counter %q: unexpected value type", name)
	}
	return strconv.ParseInt(v.Value, 10, 64)
}

// --- applications --------------------------------------------------------

func (st *Store) PutApplication(ctx context.Context, app gotify.Application) error {
	item, err := attributevalue.MarshalMap(app)
	if err != nil {
		return err
	}
	item["PK"] = s("APP")
	item["SK"] = s(padID(app.ID))
	item["tokenPK"] = s("TOKEN#" + app.Token)
	item["entityType"] = s("application")
	_, err = st.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: &st.table, Item: item})
	return err
}

func (st *Store) GetApplication(ctx context.Context, id int64) (gotify.Application, bool, error) {
	out, err := st.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &st.table, Key: appKey(id)})
	if err != nil {
		return gotify.Application{}, false, err
	}
	if out.Item == nil {
		return gotify.Application{}, false, nil
	}
	var app gotify.Application
	return app, true, attributevalue.UnmarshalMap(out.Item, &app)
}

func (st *Store) GetApplicationByToken(ctx context.Context, token string) (gotify.Application, bool, error) {
	item, ok, err := st.lookupToken(ctx, token)
	if err != nil || !ok {
		return gotify.Application{}, ok, err
	}
	var app gotify.Application
	return app, true, attributevalue.UnmarshalMap(item, &app)
}

func (st *Store) ListApplications(ctx context.Context) ([]gotify.Application, error) {
	apps := []gotify.Application{}
	err := st.queryAll(ctx, "APP", "", func(item map[string]ddbtypes.AttributeValue) error {
		var a gotify.Application
		if err := attributevalue.UnmarshalMap(item, &a); err != nil {
			return err
		}
		apps = append(apps, a)
		return nil
	})
	return apps, err
}

func (st *Store) DeleteApplication(ctx context.Context, id int64) error {
	if _, err := st.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &st.table, Key: appKey(id)}); err != nil {
		return err
	}
	return st.DeleteAppMessages(ctx, id)
}

// --- clients -------------------------------------------------------------

func (st *Store) PutClient(ctx context.Context, c gotify.Client) error {
	item, err := attributevalue.MarshalMap(c)
	if err != nil {
		return err
	}
	item["PK"] = s("CLIENT")
	item["SK"] = s(padID(c.ID))
	item["tokenPK"] = s("TOKEN#" + c.Token)
	item["entityType"] = s("client")
	_, err = st.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: &st.table, Item: item})
	return err
}

func (st *Store) GetClient(ctx context.Context, id int64) (gotify.Client, bool, error) {
	out, err := st.db.GetItem(ctx, &dynamodb.GetItemInput{TableName: &st.table, Key: clientKey(id)})
	if err != nil {
		return gotify.Client{}, false, err
	}
	if out.Item == nil {
		return gotify.Client{}, false, nil
	}
	var c gotify.Client
	return c, true, attributevalue.UnmarshalMap(out.Item, &c)
}

func (st *Store) GetClientByToken(ctx context.Context, token string) (gotify.Client, bool, error) {
	item, ok, err := st.lookupToken(ctx, token)
	if err != nil || !ok {
		return gotify.Client{}, ok, err
	}
	var c gotify.Client
	return c, true, attributevalue.UnmarshalMap(item, &c)
}

func (st *Store) ListClients(ctx context.Context) ([]gotify.Client, error) {
	clients := []gotify.Client{}
	err := st.queryAll(ctx, "CLIENT", "", func(item map[string]ddbtypes.AttributeValue) error {
		var c gotify.Client
		if err := attributevalue.UnmarshalMap(item, &c); err != nil {
			return err
		}
		clients = append(clients, c)
		return nil
	})
	return clients, err
}

func (st *Store) DeleteClient(ctx context.Context, id int64) error {
	_, err := st.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &st.table, Key: clientKey(id)})
	return err
}

// lookupToken resolves an application or client token via the byToken GSI.
func (st *Store) lookupToken(ctx context.Context, token string) (map[string]ddbtypes.AttributeValue, bool, error) {
	out, err := st.db.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &st.table,
		IndexName:                 &st.byToken,
		KeyConditionExpression:    aws.String("tokenPK = :t"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":t": s("TOKEN#" + token)},
		Limit:                     aws.Int32(1),
	})
	if err != nil {
		return nil, false, err
	}
	if len(out.Items) == 0 {
		return nil, false, nil
	}
	return out.Items[0], true, nil
}

// --- messages ------------------------------------------------------------

func (st *Store) PutMessage(ctx context.Context, m gotify.Message) error {
	item, err := attributevalue.MarshalMap(m)
	if err != nil {
		return err
	}
	item["PK"] = s("MSG")
	item["SK"] = s(padID(m.ID))
	item["appPK"] = s("APPMSG#" + strconv.FormatInt(m.ApplicationID, 10))
	item["appSK"] = s(padID(m.ID))
	if st.messageTTL > 0 {
		item["ttl"] = num(time.Now().Add(st.messageTTL).Unix())
	}
	_, err = st.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: &st.table, Item: item})
	return err
}

// ListMessages returns up to limit messages with id < since (or the newest
// when since == 0), in descending id order.
func (st *Store) ListMessages(ctx context.Context, since int64, limit int32) ([]gotify.Message, error) {
	in := &dynamodb.QueryInput{
		TableName:                 &st.table,
		KeyConditionExpression:    aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": s("MSG")},
		ScanIndexForward:          aws.Bool(false),
		Limit:                     aws.Int32(limit),
	}
	if since > 0 {
		in.KeyConditionExpression = aws.String("PK = :pk AND SK < :sk")
		in.ExpressionAttributeValues[":sk"] = s(padID(since))
	}
	return st.queryMessages(ctx, in)
}

// ListAppMessages is the per-application equivalent of ListMessages.
func (st *Store) ListAppMessages(ctx context.Context, appID, since int64, limit int32) ([]gotify.Message, error) {
	in := &dynamodb.QueryInput{
		TableName:                 &st.table,
		IndexName:                 &st.byApp,
		KeyConditionExpression:    aws.String("appPK = :pk"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": s("APPMSG#" + strconv.FormatInt(appID, 10))},
		ScanIndexForward:          aws.Bool(false),
		Limit:                     aws.Int32(limit),
	}
	if since > 0 {
		in.KeyConditionExpression = aws.String("appPK = :pk AND appSK < :sk")
		in.ExpressionAttributeValues[":sk"] = s(padID(since))
	}
	return st.queryMessages(ctx, in)
}

func (st *Store) queryMessages(ctx context.Context, in *dynamodb.QueryInput) ([]gotify.Message, error) {
	out, err := st.db.Query(ctx, in)
	if err != nil {
		return nil, err
	}
	msgs := make([]gotify.Message, 0, len(out.Items))
	for _, item := range out.Items {
		var m gotify.Message
		if err := attributevalue.UnmarshalMap(item, &m); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (st *Store) DeleteMessage(ctx context.Context, id int64) error {
	_, err := st.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &st.table, Key: msgKey(id)})
	return err
}

// DeleteAllMessages removes every message.
func (st *Store) DeleteAllMessages(ctx context.Context) error {
	var keys []map[string]ddbtypes.AttributeValue
	err := st.queryAll(ctx, "MSG", "", func(item map[string]ddbtypes.AttributeValue) error {
		keys = append(keys, map[string]ddbtypes.AttributeValue{"PK": item["PK"], "SK": item["SK"]})
		return nil
	})
	if err != nil {
		return err
	}
	return st.batchDelete(ctx, keys)
}

// DeleteAppMessages removes every message belonging to one application.
func (st *Store) DeleteAppMessages(ctx context.Context, appID int64) error {
	var keys []map[string]ddbtypes.AttributeValue
	err := st.queryAllIndex(ctx, st.byApp, "appPK", "APPMSG#"+strconv.FormatInt(appID, 10),
		func(item map[string]ddbtypes.AttributeValue) error {
			keys = append(keys, map[string]ddbtypes.AttributeValue{"PK": item["PK"], "SK": item["SK"]})
			return nil
		})
	if err != nil {
		return err
	}
	return st.batchDelete(ctx, keys)
}

// --- connections ---------------------------------------------------------

// SaveConnection records a live WebSocket connection with a TTL so dead
// sockets self-clean even if $disconnect is missed.
func (st *Store) SaveConnection(ctx context.Context, connID string, clientID int64) error {
	item := connKey(connID)
	item["clientId"] = num(clientID)
	item["connectedAt"] = num(time.Now().Unix())
	// API Gateway caps WS connections at 2h; expire a little after that.
	item["ttl"] = num(time.Now().Add(150 * time.Minute).Unix())
	_, err := st.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: &st.table, Item: item})
	return err
}

func (st *Store) DeleteConnection(ctx context.Context, connID string) error {
	_, err := st.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &st.table, Key: connKey(connID)})
	return err
}

// ListConnections returns all live connection ids (single-user: every
// connection is the user's).
func (st *Store) ListConnections(ctx context.Context) ([]string, error) {
	var ids []string
	err := st.queryAll(ctx, "CONN", "", func(item map[string]ddbtypes.AttributeValue) error {
		if sk, ok := item["SK"].(*ddbtypes.AttributeValueMemberS); ok {
			ids = append(ids, sk.Value)
		}
		return nil
	})
	return ids, err
}

// --- shared query/delete helpers ----------------------------------------

// queryAll iterates every item in a base-table partition, calling fn per item.
func (st *Store) queryAll(ctx context.Context, pk, _ string, fn func(map[string]ddbtypes.AttributeValue) error) error {
	var start map[string]ddbtypes.AttributeValue
	for {
		out, err := st.db.Query(ctx, &dynamodb.QueryInput{
			TableName:                 &st.table,
			KeyConditionExpression:    aws.String("PK = :pk"),
			ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": s(pk)},
			ExclusiveStartKey:         start,
		})
		if err != nil {
			return err
		}
		for _, item := range out.Items {
			if err := fn(item); err != nil {
				return err
			}
		}
		if out.LastEvaluatedKey == nil {
			return nil
		}
		start = out.LastEvaluatedKey
	}
}

// queryAllIndex is queryAll against a GSI partition.
func (st *Store) queryAllIndex(ctx context.Context, index, pkAttr, pk string, fn func(map[string]ddbtypes.AttributeValue) error) error {
	var start map[string]ddbtypes.AttributeValue
	for {
		out, err := st.db.Query(ctx, &dynamodb.QueryInput{
			TableName:                 &st.table,
			IndexName:                 &index,
			KeyConditionExpression:    aws.String("#pk = :pk"),
			ExpressionAttributeNames:  map[string]string{"#pk": pkAttr},
			ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{":pk": s(pk)},
			ExclusiveStartKey:         start,
		})
		if err != nil {
			return err
		}
		for _, item := range out.Items {
			if err := fn(item); err != nil {
				return err
			}
		}
		if out.LastEvaluatedKey == nil {
			return nil
		}
		start = out.LastEvaluatedKey
	}
}

// batchDelete deletes keys in chunks of 25 (the BatchWriteItem limit).
func (st *Store) batchDelete(ctx context.Context, keys []map[string]ddbtypes.AttributeValue) error {
	for i := 0; i < len(keys); i += 25 {
		end := i + 25
		if end > len(keys) {
			end = len(keys)
		}
		reqs := make([]ddbtypes.WriteRequest, 0, end-i)
		for _, k := range keys[i:end] {
			reqs = append(reqs, ddbtypes.WriteRequest{DeleteRequest: &ddbtypes.DeleteRequest{Key: k}})
		}
		if _, err := st.db.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]ddbtypes.WriteRequest{st.table: reqs},
		}); err != nil {
			return err
		}
	}
	return nil
}

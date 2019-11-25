package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

const (
	database   = "cogman"
	tableTasks = "tasks"
)

// MongoClient contaiend required field
type MongoClient struct {
	URL    string
	ExpDur int32
	mcl    *mongo.Client
	sess   map[string]mongo.Session
}

// NewMongoClient return a mongo client
func NewMongoClient(url string, ttl time.Duration) (*MongoClient, error) {
	conn, err := mongo.Connect(
		context.Background(),
		options.Client().ApplyURI(url),
	)

	if err != nil {
		return nil, err
	}

	return &MongoClient{
		URL:    url,
		ExpDur: int32(ttl.Seconds()),
		mcl:    conn,
		sess:   make(map[string]mongo.Session),
	}, nil
}

func (m *MongoClient) StartTransaction(id string) (interface{}, error) {
	sess, err := m.mcl.StartSession()
	if err != nil {
		return nil, err
	}

	m.sess[id] = sess
	return nil, m.sess[id].StartTransaction()
}

func (m *MongoClient) CommitTransaction(id string, ctx context.Context) (interface{}, error) {
	err := m.sess[id].CommitTransaction(ctx)
	if err != nil {
		return nil, err
	}
	m.sess[id].EndSession(ctx)
	delete(m.sess, id)
	return nil, nil
}

// Ping check the mongo connection status
func (m *MongoClient) Ping() error {
	return m.mcl.Ping(context.Background(), readpref.Primary())
}

// SetTTL for mongo object
func (m *MongoClient) SetTTL() (interface{}, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return nil, err
	}

	col.Indexes().DropOne(ctx, "TTL")

	opts := &options.IndexOptions{}
	opts.SetName("TTL")
	opts.SetExpireAfterSeconds(m.ExpDur)

	model := mongo.IndexModel{
		Keys: bson.D{
			bson.E{"created_at", 1},
		},
		Options: opts,
	}

	return col.Indexes().CreateOne(ctx, model)
}

// IndexKey holds a key of index
type IndexKey struct {
	Key  string
	Desc bool
}

// Index reprsents a mongodb index
type Index struct {
	Keys   []IndexKey
	Name   string
	Unique bool
	Sparse bool
}

func (i *Index) model() mongo.IndexModel {
	keys := bson.D{}
	for _, k := range i.Keys {
		d := 1
		if k.Desc {
			d = -1
		}
		keys = append(keys, bson.E{k.Key, d})
	}

	opts := &options.IndexOptions{}
	if i.Name != "" {
		opts.SetName(i.Name)
	}
	opts.SetSparse(i.Sparse)
	opts.SetUnique(i.Unique)

	m := mongo.IndexModel{
		Keys:    keys,
		Options: opts,
	}

	return m
}

// EnsureIndices ensure mongo index list
func (m *MongoClient) EnsureIndices(indices []Index) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return err
	}

	models := []mongo.IndexModel{}
	for _, ind := range indices {
		models = append(models, ind.model())
	}

	if _, err := col.Indexes().CreateMany(ctx, models); err != nil {
		return err
	}

	return nil
}

// DropIndices drop previous mongo field
func (m *MongoClient) DropIndices() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return err
	}

	_, err = col.Indexes().DropAll(ctx)
	return err
}

// Close close the mongo connection
func (m *MongoClient) Close() error {
	return m.mcl.Disconnect(context.Background())
}

// Connect initiate a mongo connection
func (m *MongoClient) Connect() error {
	err := m.mcl.Connect(context.Background())
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoClient) getCollection() (*mongo.Collection, error) {
	return m.mcl.Database(database).Collection(tableTasks), nil
}

// Get return a single object based on query parameter
func (m *MongoClient) Get(q bson.M) (*mongo.SingleResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return nil, err
	}

	resp := col.FindOne(ctx, q)
	if resp.Err() != nil {
		return nil, resp.Err()
	}

	return resp, nil
}

// Create create a object
func (m *MongoClient) Create(t interface{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return err
	}

	_, err = col.InsertOne(ctx, t)
	return err
}

// Update update a object
func (m *MongoClient) Update(q, val interface{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return err
	}

	resp, err := col.ReplaceOne(ctx, q, val)
	if err != nil {
		return err
	}
	if resp.MatchedCount == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdatePartial update a object partially
func (m *MongoClient) UpdatePartial(q, val interface{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return err
	}

	resp, err := col.UpdateOne(ctx, q, val)
	if err != nil {
		return err
	}
	if resp.MatchedCount == 0 {
		return ErrNotFound
	}

	return nil
}

// List return a list of object based on query parameter
func (m *MongoClient) List(q interface{}, skip, limit int) (*mongo.Cursor, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return nil, err
	}

	opt := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit))
	cursor, err := col.Find(ctx, q, opt)
	if err != nil {
		return nil, err
	}
	if cursor.Err() != nil {
		return nil, cursor.Err()
	}

	return cursor, nil
}

// Aggregate return a Cursor
func (m *MongoClient) Aggregate(q interface{}) (*mongo.Cursor, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	col, err := m.getCollection()
	if err != nil {
		return nil, err
	}

	cursor, err := col.Aggregate(ctx, q)
	if err != nil {
		return nil, err
	}
	if cursor.Err() != nil {
		return nil, cursor.Err()
	}

	return cursor, nil
}

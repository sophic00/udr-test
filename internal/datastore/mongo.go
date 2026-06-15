package datastore

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Resource struct {
	ID        string    `bson:"_id"`
	Data      bson.M    `bson:"data"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

type Datastore struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
}

func NewDatastore(ctx context.Context, uri, dbName string) (*Datastore, error) {
	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, err
	}

	db := client.Database(dbName)
	collection := db.Collection("resources")

	log.Printf("Connected to MongoDB at %s, database %s", uri, dbName)

	return &Datastore{
		client:     client,
		db:         db,
		collection: collection,
	}, nil
}

func (d *Datastore) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}

func (d *Datastore) Get(ctx context.Context, id string) (bson.M, error) {
	var res Resource
	err := d.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&res)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return res.Data, nil
}

func (d *Datastore) Put(ctx context.Context, id string, data bson.M) error {
	opts := options.Update().SetUpsert(true)
	update := bson.M{
		"$set": bson.M{
			"data":      data,
			"updatedAt": time.Now(),
		},
	}
	_, err := d.collection.UpdateByID(ctx, id, update, opts)
	return err
}

func (d *Datastore) Delete(ctx context.Context, id string) error {
	_, err := d.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (d *Datastore) List(ctx context.Context, prefix string) ([]bson.M, error) {
	// Query resources where ID starts with prefix
	filter := bson.M{
		"_id": bson.M{
			"$regex": "^" + strings.ReplaceAll(prefix, "/", "\\/"),
		},
	}
	cursor, err := d.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	for cursor.Next(ctx) {
		var res Resource
		if err := cursor.Decode(&res); err != nil {
			return nil, err
		}
		results = append(results, res.Data)
	}

	return results, nil
}

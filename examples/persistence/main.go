package main

import (
	"context"
	"fmt"
	"log"

	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/postgres"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// UserProfile is an example of a persistent distributed object
type UserProfile struct {
	object.BaseObject
	Username string
	Email    string
	Score    int
}

// OnCreated is called when the object is created
func (u *UserProfile) OnCreated() {
	u.Logger.Infof("UserProfile created: %s", u.Id())
}

// ToData serializes the UserProfile to a proto.Message for persistence
func (u *UserProfile) ToData() (proto.Message, error) {
	data, err := structpb.NewStruct(map[string]interface{}{
		"id":       u.Id(),
		"type":     u.Type(),
		"username": u.Username,
		"email":    u.Email,
		"score":    u.Score,
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FromData deserializes the UserProfile from a proto.Message
func (u *UserProfile) FromData(data proto.Message) error {
	structData, ok := data.(*structpb.Struct)
	if !ok {
		return nil
	}
	if username, ok := structData.Fields["username"]; ok {
		u.Username = username.GetStringValue()
	}
	if email, ok := structData.Fields["email"]; ok {
		u.Email = email.GetStringValue()
	}
	if score, ok := structData.Fields["score"]; ok {
		u.Score = int(score.GetNumberValue())
	}

	return nil
}

func main() {
	fmt.Println("=== Goverse Object Persistence Example ===")

	// This example demonstrates how to use PostgreSQL persistence with Goverse objects
	// Note: This requires a running PostgreSQL database
	// See docs/postgres-setup.md for setup instructions

	// Create database configuration
	config := &postgres.Config{
		Host:     "localhost",
		Port:     5432,
		User:     "goverse",
		Password: "goverse",
		Database: "goverse",
		SSLMode:  "disable",
	}

	// Connect to database
	fmt.Println("Connecting to PostgreSQL...")
	db, err := postgres.NewDB(config)
	if err != nil {
		log.Printf("Failed to connect to database: %v\n", err)
		log.Println("This example requires a running PostgreSQL database.")
		log.Println("See docs/postgres-setup.md for setup instructions.")
		return
	}
	defer db.Close()

	// Verify connection
	ctx := context.Background()
	err = db.Ping(ctx)
	if err != nil {
		log.Printf("Failed to ping database: %v\n", err)
		log.Println("This example requires a running PostgreSQL database.")
		log.Println("See docs/postgres-setup.md for setup instructions.")
		return
	}

	// Initialize schema
	fmt.Println("Initializing database schema...")
	err = db.InitSchema(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize schema: %v", err)
	}

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Example 1: Create and save a persistent object
	fmt.Println("\n--- Example 1: Creating and Saving a Persistent Object ---")

	user1 := &UserProfile{}
	user1.OnInit(user1, "user-alice")
	user1.Username = "alice"
	user1.Email = "alice@example.com"
	user1.Score = 100

	fmt.Printf("Created UserProfile: %s\n", user1.Id())
	fmt.Printf("  Username: %s\n", user1.Username)
	fmt.Printf("  Email: %s\n", user1.Email)
	fmt.Printf("  Score: %d\n", user1.Score)

	data, err := user1.ToData()
	if err != nil {
		log.Fatalf("Failed to get object data: %v", err)
	}
	err = object.SaveObject(ctx, provider, user1.Id(), user1.Type(), data)
	if err != nil {
		log.Fatalf("Failed to save object: %v", err)
	}
	fmt.Println("✓ Object saved to database")

	// Example 2: Load a persistent object
	fmt.Println("\n--- Example 2: Loading a Persistent Object ---")

	user2 := &UserProfile{}
	user2.OnInit(user2, "user-alice")

	// Get a proto.Message to load into
	loadedData, err := user2.ToData()
	if err != nil {
		log.Fatalf("Failed to create data template: %v", err)
	}

	err = object.LoadObject(ctx, provider, "user-alice", loadedData)
	if err != nil {
		log.Fatalf("Failed to load object: %v", err)
	}

	// Restore the object state from loaded data
	err = user2.FromData(loadedData)
	if err != nil {
		log.Fatalf("Failed to restore object from data: %v", err)
	}

	fmt.Printf("Loaded UserProfile: %s\n", user2.Id())
	fmt.Printf("  Username: %s\n", user2.Username)
	fmt.Printf("  Email: %s\n", user2.Email)
	fmt.Printf("  Score: %d\n", user2.Score)
	fmt.Println("✓ Object loaded from database")

	// Example 3: Update and save
	fmt.Println("\n--- Example 3: Updating and Saving ---")

	user2.Score += 50
	user2.Email = "alice.updated@example.com"

	fmt.Printf("Updated UserProfile: %s\n", user2.Id())
	fmt.Printf("  Email: %s\n", user2.Email)
	fmt.Printf("  Score: %d\n", user2.Score)

	data, err = user2.ToData()
	if err != nil {
		log.Fatalf("Failed to get object data: %v", err)
	}
	err = object.SaveObject(ctx, provider, user2.Id(), user2.Type(), data)
	if err != nil {
		log.Fatalf("Failed to update object: %v", err)
	}
	fmt.Println("✓ Object updated in database")

	// Example 4: Create multiple objects
	fmt.Println("\n--- Example 4: Creating Multiple Objects ---")

	users := []struct {
		id       string
		username string
		email    string
		score    int
	}{
		{"user-bob", "bob", "bob@example.com", 200},
		{"user-charlie", "charlie", "charlie@example.com", 150},
	}

	for _, u := range users {
		userObj := &UserProfile{}
		userObj.OnInit(userObj, u.id)
		userObj.Username = u.username
		userObj.Email = u.email
		userObj.Score = u.score

		data, err := userObj.ToData()
		if err != nil {
			log.Fatalf("Failed to get object data for %s: %v", u.id, err)
		}
		err = object.SaveObject(ctx, provider, userObj.Id(), userObj.Type(), data)
		if err != nil {
			log.Fatalf("Failed to save user %s: %v", u.id, err)
		}
		fmt.Printf("✓ Saved user: %s (score: %d)\n", u.username, u.score)
	}

	// Example 5: List all objects of a type
	fmt.Println("\n--- Example 5: Listing All UserProfile Objects ---")

	objects, err := db.ListObjectsByType(ctx, "UserProfile")
	if err != nil {
		log.Fatalf("Failed to list objects: %v", err)
	}

	fmt.Printf("Found %d UserProfile objects:\n", len(objects))
	for i, obj := range objects {
		// Unmarshal the JSONB data
		structData := &structpb.Struct{}
		err := object.UnmarshalProtoFromJSON(obj.Data, structData)
		if err == nil {
			username := structData.Fields["username"].GetStringValue()
			score := int(structData.Fields["score"].GetNumberValue())
			fmt.Printf("  %d. %s - Username: %s, Score: %d\n", i+1, obj.ObjectID, username, score)
		} else {
			fmt.Printf("  %d. %s - (failed to unmarshal data)\n", i+1, obj.ObjectID)
		}
	}

	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("1. Persistent objects extend BaseObject and override ToData()/FromData()")
	fmt.Println("2. Non-persistent objects just use the default BaseObject implementation")
	fmt.Println("3. Use SaveObject() and LoadObject() - they handle persistence automatically")
	fmt.Println("4. PostgreSQL stores object state as JSONB with automatic timestamps")
}

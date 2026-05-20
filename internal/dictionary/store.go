package dictionary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Store is the persistent storage contract.
type Store interface {
	AddWord(ctx context.Context, clientID, word, language string) error
	RemoveWord(ctx context.Context, clientID, word string) error
	ListWords(ctx context.Context, clientID string) ([]Word, error)
	ClearAll(ctx context.Context, clientID string) error
}

type dynamoItem struct {
	PK       string `dynamodbav:"PK"`
	SK       string `dynamodbav:"SK"`
	Word     string `dynamodbav:"word"`
	Language string `dynamodbav:"language,omitempty"`
	AddedAt  string `dynamodbav:"addedAt"`
}

// DynamoStore implements Store using aws-sdk-go-v2.
type DynamoStore struct {
	client    *dynamodb.Client
	tableName string
}

func NewDynamoStore(client *dynamodb.Client, tableName string) *DynamoStore {
	return &DynamoStore{client: client, tableName: tableName}
}

func pk(clientID string) string { return "CLIENT#" + clientID }
func sk(word string) string   { return "WORD#" + strings.ToLower(word) }

func (s *DynamoStore) AddWord(ctx context.Context, clientID, word, language string) error {
	item, err := attributevalue.MarshalMap(dynamoItem{
		PK:       pk(clientID),
		SK:       sk(word),
		Word:     word,
		Language: language,
		AddedAt:  time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	return err
}

func (s *DynamoStore) RemoveWord(ctx context.Context, clientID, word string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk(clientID)},
			"SK": &types.AttributeValueMemberS{Value: sk(word)},
		},
	})
	return err
}

func (s *DynamoStore) ListWords(ctx context.Context, clientID string) ([]Word, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: pk(clientID)},
			":prefix": &types.AttributeValueMemberS{Value: "WORD#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	words := make([]Word, 0, len(out.Items))
	for _, item := range out.Items {
		var di dynamoItem
		if err := attributevalue.UnmarshalMap(item, &di); err != nil {
			continue
		}
		t, _ := time.Parse(time.RFC3339, di.AddedAt)
		words = append(words, Word{
			Word:     di.Word,
			Language: di.Language,
			AddedAt:  t,
		})
	}
	return words, nil
}

func (s *DynamoStore) ClearAll(ctx context.Context, clientID string) error {
	words, err := s.ListWords(ctx, clientID)
	if err != nil {
		return err
	}
	for _, w := range words {
		if err := s.RemoveWord(ctx, clientID, w.Word); err != nil {
			return err
		}
	}
	return nil
}

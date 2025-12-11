package secrets

import (
	"context"
	"fmt"
	"log"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

// GetSecret retrieves a secret from Google Secret Manager
func GetSecret(projectID, secretName string) (string, error) {
	ctx := context.Background()

	// Create the client
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create secretmanager client: %w", err)
	}
	defer client.Close()

	// Build the secret version name
	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName)

	// Access the secret version
	result, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		return "", fmt.Errorf("failed to access secret version: %w", err)
	}

	log.Printf("[Secrets] Retrieved secret: %s", secretName)
	return string(result.Payload.Data), nil
}

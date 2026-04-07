package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// AWSCredResolver resolves AWS credentials fresh on each request using the
// default credential chain. No credentials are cached in memory — the SDK
// reads from ~/.aws/sso/cache/ or env vars on each call.
type AWSCredResolver struct{}

// ResolveCredentials loads AWS credentials for the given profile and region.
// Returns the credentials and a v4 signer. Credentials are never logged.
func (r *AWSCredResolver) ResolveCredentials(ctx context.Context, profile, region string) (aws.Credentials, *v4.Signer, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Credentials{}, nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, nil, classifyAWSError(err, profile)
	}

	signer := v4.NewSigner()
	return creds, signer, nil
}

// classifyAWSError converts raw AWS errors into user-friendly messages.
// Credential values are NEVER included in error messages.
func classifyAWSError(err error, profile string) error {
	msg := err.Error()

	if strings.Contains(msg, "ExpiredToken") ||
		strings.Contains(msg, "ExpiredTokenException") ||
		strings.Contains(msg, "UnrecognizedClientException") ||
		strings.Contains(msg, "token has expired") ||
		strings.Contains(msg, "SSO session") {

		hint := "aws sso login"
		if profile != "" {
			hint = fmt.Sprintf("aws sso login --profile %s", profile)
		}
		return fmt.Errorf("AWS SSO session expired. Run '%s' to re-authenticate", hint)
	}

	if strings.Contains(msg, "NoCredentialProviders") ||
		strings.Contains(msg, "no EC2 IMDS") {
		return fmt.Errorf("no AWS credentials found — configure AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, AWS_PROFILE, or an IAM role")
	}

	// Redact any credential-like content before returning
	slog.Debug("[proxy] AWS credential error (details redacted)", "profile", profile)
	return fmt.Errorf("AWS credential error: %w", err)
}

// IsSigV4Request returns true if the request uses SigV4 authentication.
func IsSigV4Request(authHeader string) bool {
	return strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256")
}

// SigV4Headers are the headers that SigV4 adds and must be stripped before re-signing.
var SigV4Headers = []string{
	"Authorization",
	"X-Amz-Date",
	"X-Amz-Security-Token",
	"X-Amz-Content-Sha256",
}

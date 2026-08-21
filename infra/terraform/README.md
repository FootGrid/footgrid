# Terraform layout

Terraform is split by environment. Apply only from an environment directory,
for example `infra/terraform/envs/dev`.

The initial AWS build should provision:

1. VPC, private subnets, security groups, VPC endpoints and RDS Proxy.
2. Aurora PostgreSQL Serverless v2 with encrypted backups and Secrets Manager.
3. Cognito user pool and HTTP API JWT authorizer.
4. ARM64 Go Lambda functions for identity, match and projection services.
5. EventBridge bus, per-consumer SQS queue and DLQ.
6. CloudWatch alarms for errors, event-write latency, outbox age and DLQ depth.

Use a remote S3/DynamoDB Terraform backend before the first shared deployment.
Do not commit `*.tfvars`, state files, database credentials, or Cognito client
secrets.

locals {
  common_tags = {
    Project     = var.project
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}

# Add modules here in this order: network, database, identity, api, async.
# Module implementation is intentionally deferred until AWS account IDs, DNS
# domain, and production network CIDRs are confirmed.

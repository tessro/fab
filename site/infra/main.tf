terraform {
  required_version = ">= 1.0"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.0"
    }
  }
}

variable "cloudflare_api_token" {
  description = "Cloudflare API token with Zone and Pages permissions"
  type        = string
  sensitive   = true
}

variable "cloudflare_account_id" {
  description = "Cloudflare account ID"
  type        = string
}

variable "cloudflare_zone_id" {
  description = "Cloudflare zone ID for tessro.ai"
  type        = string
}

provider "cloudflare" {
  api_token = var.cloudflare_api_token
}

# Cloudflare Pages project for the static site
resource "cloudflare_pages_project" "fab" {
  account_id        = var.cloudflare_account_id
  name              = "fab"
  production_branch = "main"

  source {
    type = "github"
    config {
      owner                         = "tessro"
      repo_name                     = "fab"
      production_branch             = "main"
      deployments_enabled           = true
      production_deployment_enabled = true
      preview_deployment_setting    = "custom"
      preview_branch_includes       = ["dev", "preview/*"]
    }
  }

  build_config {
    build_command   = ""
    destination_dir = "site/public"
  }
}

# Custom domain for fab.tessro.ai
resource "cloudflare_pages_domain" "fab" {
  account_id   = var.cloudflare_account_id
  project_name = cloudflare_pages_project.fab.name
  domain       = "fab.tessro.ai"
}

# DNS CNAME record pointing to Cloudflare Pages
resource "cloudflare_record" "fab" {
  zone_id = var.cloudflare_zone_id
  name    = "fab"
  content = "${cloudflare_pages_project.fab.name}.pages.dev"
  type    = "CNAME"
  proxied = true
}

output "pages_url" {
  description = "Cloudflare Pages URL"
  value       = "https://${cloudflare_pages_project.fab.subdomain}"
}

output "custom_domain" {
  description = "Custom domain URL"
  value       = "https://fab.tessro.ai"
}

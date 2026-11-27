terraform {
  required_version = ">= 1.11"

  required_providers {
    routeros = {
      source  = "terraform-routeros/routeros"
      version = "= 2.5.0"
    }
  }

  backend "s3" {
    bucket       = "example-router-state"
    key          = "routers/branch-a/terraform.tfstate"
    region       = "eu-central-1"
    use_lockfile = true
  }
}

provider "routeros" {
  hosturl  = "https://branch-a.example"
  username = "terraform"
}

module "router" {
  source = "../modules/router"

  monitoring_address = "192.0.2.10"
}

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}
}

resource "azurerm_resource_group" "rg" {
  name     = "gh-ghes-2-ghec-rg"
  location = "East US"
}

resource "azurerm_service_plan" "plan" {
  name                = "gh-ghes-2-ghec-plan"
  resource_group_name = azurerm_resource_group.rg.name
  location           = azurerm_resource_group.rg.location
  os_type            = "Linux"
  sku_name           = "P1v2"
}

resource "azurerm_linux_web_app" "app" {
  name                = "gh-ghes-2-ghec-app"
  resource_group_name = azurerm_resource_group.rg.name
  location           = azurerm_resource_group.rg.location
  service_plan_id    = azurerm_service_plan.plan.id

  site_config {
    application_stack {
      docker_image_name = "ghcr.io/${var.github_owner}/gh-ghes-2-ghec:latest"
    }
    always_on = true
  }

  app_settings = {
    "WEBSITES_ENABLE_APP_SERVICE_STORAGE" = "false"
    "DOCKER_REGISTRY_SERVER_URL"          = "https://ghcr.io"
    "DOCKER_REGISTRY_SERVER_USERNAME"     = var.github_username
    "DOCKER_REGISTRY_SERVER_PASSWORD"     = var.github_token
  }
} 
terraform {
  required_version = ">= 1.6.0"

  cloud {
    # TFE in HAL is exposed through the local proxy on 8443.
    hostname     = "tfe.localhost:8443"
    organization = "hal"

    workspaces {
      name = "testmiche-cli"
    }
  }
}

locals {
  hello_message = "hello from local terraform cli with remote TFE execution"
}

output "hello" {
  value = local.hello_message
}

output "run_time_utc" {
  value = formatdate("YYYY-MM-DD hh:mm:ss ZZZ", timestamp())
}

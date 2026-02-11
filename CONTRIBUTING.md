# Contributing

## Requirements

- [Go](https://golang.org/doc/install) 1.26 or higher
- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.5.7 or higher

## Building

```shell
make build
```

## Installing

For local development and testing, you can install the provider to your local Terraform plugin directory:

```sh
make install
```

## Use the Locally Installed Version

Update your `~/.terraformrc` file with the following content:

```terraform
provider_installation {
  filesystem_mirror {
    path = "/Users/{{USER_NAME}}/terraform-provider-mirror"
  }
}
```

**Note:** Make sure the locally installed version number (see `Makefile`) matches what is in your `main.tf` file.

## Releasing

```sh
git tag -a vX.X.X -m vX.X.X
git push origin --tags
```

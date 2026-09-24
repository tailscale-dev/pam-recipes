# Tailscale PAM Demo

This repo demonstrates some simple use cases for Tailscale PAM, where you have a database with customer data and you need to be able to control access for some database administrators.

The code in this repo should be treated as an example, rather than a runnable demo. If you wish to run it locally you will need to adjust the infrastructure and policy file to fit your setup.

## Policy and grants

The [policy.hujson](./policy.hujson) provides an example of

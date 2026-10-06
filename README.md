# Tailscale PAM Recipes

This repo provides recipes to help get you going with
[Tailscale Privileged Access Management (PAM)](https://tailscale.com/use-cases/pam).

Each recipe consists of:
* A README file giving an overview of how it works, and with ideas for taking it further.
* A complete `policy.hujson` file which implements the various grants, these can be taken
and adapted for your needs.

There may also be other supporting files to help get you going.

## Recipes

* [01 - Using PAM with the Amazon Web Services cloud (RDS, and S3)](./01-aws).
* [02 - Internal web apps and forwarded identity](./02-http-identity-headers).
* [03 - Fine-grained SSH with device posture](./03-ssh-device-posture).

## Getting started

These are designed to compliment the
[Tailscale PAM documentation](https://tailscale.com/docs/privileged-access-management). If
you haven't already, take a moment to read
["What is Tailscale PAM?"](https://tailscale.com/docs/privileged-access-management/what-is-tailscale-pam)
and ["Tailscale PAM architecture and core concepts"](https://tailscale.com/docs/privileged-access-management/architecture-and-concepts).

After that, these recipes can be used as a starting point for your own configuration.

## How to use the `policy.hujson` files

Tailscale governs the access between your nodes using a
[policy](https://tailscale.com/docs/features/tailnet-policy-file). These
policies define how traffic is allowed to flow through your tailnet, including
who can access PAM services.

The examples in this project provide these policies as hujson files. They are
provided as complete examples with groups and users, you will need to change
these to fit your needs before you apply them.

Once you've edited the policy, or pulled in the segments you need, you can
upload it in the [Access control JSON editor of your tailscale admin
console](https://console.tailscale.com/admin/acls/file).

## License

Contributions are licensed under the [BSD 3-Clause License](./LICENSE).

# Tailscale PAM recipes

This repo provides recipes to help you get started with
[Tailscale Privileged Access Management (PAM)](https://tailscale.com/use-cases/pam).

> [!NOTE]
> Tailscale PAM is currently in beta. For more information, refer to
> [Tailscale release stages](https://tailscale.com/docs/reference/tailscale-release-stages).
> Some recipes also depend on plan-specific features. For example, the
> [device posture recipe](./03-ssh-device-posture) uses custom posture
> attributes, which require the Premium or Enterprise plan. For details, refer
> to [Tailscale pricing](https://tailscale.com/pricing).

Each recipe consists of:
- A README file giving an overview of how it works, and with ideas for taking it further.
- A complete `policy.hujson` file that implements the grants. You can adapt it
for your needs.

Some recipes also include other supporting files.

## Recipes

- [01 - Using PAM with Amazon Web Services (Amazon RDS and Amazon S3)](./01-aws).
- [02 - Internal web apps and forwarded identity](./02-http-identity-headers).
- [03 - Fine-grained SSH with device posture](./03-ssh-device-posture).

## Get started

These recipes complement the
[Tailscale PAM documentation](https://tailscale.com/docs/privileged-access-management). If
you haven't already, take a moment to read
["What is Tailscale PAM?"](https://tailscale.com/docs/privileged-access-management/what-is-tailscale-pam)
and ["Tailscale PAM architecture and core concepts"](https://tailscale.com/docs/privileged-access-management/architecture-and-concepts).

Every recipe uses a PAM connector: a host that sits alongside the resources you
want to protect, holds the upstream credentials, and brokers access to the
services in front of them. Tailzero is the application that runs a PAM
connector. You install it on the host and join it to your Tailscale network
(known as a tailnet).

After that, you can use these recipes as a starting point for your own
configuration.

## How to use the `policy.hujson` files

Tailscale governs the access between your nodes using a
[policy](https://tailscale.com/docs/features/tailnet-policy-file). These
policies define how traffic is allowed to flow through your tailnet, including
who can access PAM services.

Each recipe provides its policy as a huJSON file. Each file is a complete
example, including groups and users. Change these to fit your needs before you
apply them.

After you edit the policy, or copy in the segments you need, you can
upload it on the [**Access controls**](https://console.tailscale.com/admin/acls/file)
page of the admin console.

## License

Contributions are licensed under the [BSD 3-Clause License](./LICENSE).

# Security Policy

Argo Watcher sits between CI pipelines and Argo CD and holds Argo CD credentials, deploy tokens, and Git write access. Vulnerabilities in it are taken seriously, and genuine reports are welcome.

## Supported Versions

Only the latest released version is supported. Fixes land in a new release; there are no backports to older tags, and no long-term-support branch. Before reporting, confirm the issue still reproduces on the most recent release.

## Reporting a Vulnerability

**Do not open a public issue, pull request, or discussion for a security problem.**

Report it privately through GitHub:

**<https://github.com/shini4i/argo-watcher/security/advisories/new>**

(Or: repository → **Security** tab → **Report a vulnerability**.)

A useful report includes:

- The affected version, commit, or container image tag.
- The component: server, client, GitOps updater, web UI, or Helm/deployment config.
- The configuration required to trigger it — authentication mode, `STATE_TYPE`, whether write-back is enabled, and any relevant environment variables.
- Concrete steps to reproduce, ideally a request sequence or a minimal script.
- The impact you can demonstrate, not the impact you suspect.

## What to Expect

This is a single-maintainer hobby project worked on outside of business hours. Everything below describes what usually happens, not what is promised:

- Reports are read and answered when time allows, but there is no guaranteed response time and a report may sit for a while.
- Triage and fixes are best-effort and prioritised by demonstrated impact. Some findings will be accepted as known trade-offs and not fixed.
- A fix may be published as a GitHub Security Advisory when the impact warrants it, and reporters who want credit are usually named. Neither is automatic.
- Reporters are free to disclose publicly on their own schedule. Coordinating a date is appreciated, but nothing here obliges you to wait.

Nothing in this policy is a service commitment or a warranty. The software is provided as-is under its [license](../LICENSE).

## Scope

In scope: the code in this repository — the server, the CLI client, the GitOps updater, the web UI, the published container images, and the CI workflows.

Out of scope:

- Argo CD, Kubernetes, PostgreSQL, or any other third-party component. Report those to their own maintainers.
- Raw scanner or fuzzer output with no demonstrated exploit path against a realistic deployment.
- Issues that require an already-compromised host, cluster-admin access, or a maliciously crafted local configuration file.
- Dependency advisories already tracked by Dependabot, or already documented as accepted with a rationale (see [`.trivyignore`](../.trivyignore)). If you believe an accepted rationale is wrong, that argument is welcome — as a normal issue, not an advisory.
- Deployment mistakes in a specific installation, such as exposing an unauthenticated instance to the public internet. Hardening guidance belongs in the [documentation](https://argo-watcher.readthedocs.io/), and doc gaps are worth an issue.

## No Bug Bounty, and No Solicitations

There is no bug bounty. This project pays no rewards, bounties, swag, or "recognition fees", and it issues no certificates, letters of appreciation, or hall-of-fame entries beyond the advisory credit described above. Nothing in this policy creates an expectation of payment.

The following are not security reports and are closed without a reply, then reported to GitHub as spam:

- Offers to sell or trial scanning tools, "hacking programs", pentest services, audits, or security consulting.
- Advisory or issue threads whose actual content is an advertisement, a referral link, or a request for contact off-platform.
- Reports demanding payment before details are shared.
- Bulk automated submissions, or the same low-quality finding filed across many unrelated repositories.

Persistent senders are blocked from the repository.

None of the above applies to good-faith reports. Those are welcome — including from first-time reporters, and including ones that turn out to be wrong.

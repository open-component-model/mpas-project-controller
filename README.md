# MPAS Project Controller

[![REUSE status](https://api.reuse.software/badge/github.com/open-component-model/mpas-project-controller)](https://api.reuse.software/info/github.com/open-component-model/mpas-project-controller)

The MPAS Project controller is part of the Multi-Platform Automation System (MPAS). It is a Kubernetes controller that manages the lifecycle of MPAS projects. It is responsible for creating and deleting projects and managing the project's resources. MPAS, and the Project Controller, are designed to be used with the [Open Component Model](https://ocm.software) and [Flux](https://fluxcd.io).

The project controller provides a Project Custom Resource Definition (CRD) to enable the following features:

- Create a Kubernetes namespace for project resources.
- Create a Project ServiceAccount and associated RBAC.
- Create a git repository for the project. GitHub, GitLab, and Gitea are supported.
  - The repository is bootstrapped with the necessary folder structure and files to enable Flux to manage the project.
  - Project owners can specify `maintainers` for the repository, which will automatically be added to the `CODEOWNERS` file.
- A Flux GitRepository source is created for the project Git repository created above.
- Flux Kustomizations are configured for each of the bootstrapped folders in the project Git repository.

## Quick Start

Prerequisites:

- Docker, KIND, and kubectl are required.
- Create a kind cluster: `kind create cluster`
- Install the `git-controller`: see [open-component-model/git-controller](https://github.com/open-component-model/git-controller)
- Install Flux by running `flux install`
- Install the Project Controller

---

In this tutorial, we'll create a project called "my-project" that will be managed by Flux. A GitHub repository will be created for the project, and Flux will be configured to manage the project's resources.

To get started create a `Secret` with your GitHub credentials. The password should be a [personal access token](https://docs.github.com/en/github/authenticating-to-github/creating-a-personal-access-token) with `repo` permissions. The username and password should be base64 encoded.

```yaml
# github-creds.yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-creds
  namespace: mpas-system
type: Opaque
data:
  username: <base64 encoded username>
  password: <base64 encoded password>
```

Apply the secret to the cluster:

```bash
kubectl apply -f github-creds.yaml
```

Create a `Project` resource:

```yaml
# my-project.yaml
apiVersion: mpas.ocm.software/v1alpha1
kind: Project
metadata:
  name: my-project
  namespace: mpas-system
spec:
  flux:
    interval: 5m
  git:
    provider: github
    owner: <github_username>
    isOrganization: false
    maintainers:
    - <github_username>
    visibility: private
    existingRepositorypolicy: adopt
    credentials:
      secretRef:
        name: github-creds
  prune: true
```

Apply the project to the cluster:

```bash
kubectl apply -f my-project.yaml
```

The project controller will create a namespace for the project, a service account, and RBAC. It will also create a GitHub repository for the project, and configure Flux to manage the project's resources.

View the resources created by the project controller:

```bash
kubectl get ns mpas-my-project
kubectl get sa,rolebinding -n mpas-my-project
kubectl get repositories,gitrepositories,kustomizations,rolebindings -n mpas-system
```

Expected output:

```bash
$ kubectl get ns mpas-my-project
NAME                STATUS   AGE
mpas-my-project   Active   1m

$ kubectl get sa,rolebinding -n mpas-my-project
NAME                               SECRETS   AGE
serviceaccount/default             0         1m
serviceaccount/mpas-my-project   0         1m

NAME                                                                  ROLE                                    AGE
rolebinding.rbac.authorization.k8s.io/mpas-my-project               Role/mpas-my-project                  1m
rolebinding.rbac.authorization.k8s.io/mpas-my-project-clusterrole   ClusterRole/mpas-projects-clusterrole   1m

$ kubectl get repositories,gitrepositories,kustomizations,rolebindings -n mpas-system
NAME                                             AGE
repository.mpas.ocm.software/mpas-my-project   1m

NAME                                                       URL                           AGE   READY   STATUS
gitrepository.source.toolkit.fluxcd.io/mpas-my-project   https://github.com/open-component-model/mpas-my-project   1m    True   stored artifact for revision 'main@sha1:112e4b27aa05b114a3adfe1b16d81bf49706ab42'

NAME                                                                        AGE   READY   STATUS
kustomization.kustomize.toolkit.fluxcd.io/mpas-my-project-generators      1m    True   Source is not ready, artifact not found
kustomization.kustomize.toolkit.fluxcd.io/mpas-my-project-products        1m    True   Source is not ready, artifact not found
kustomization.kustomize.toolkit.fluxcd.io/mpas-my-project-subscriptions   1m    True   Source is not ready, artifact not found
kustomization.kustomize.toolkit.fluxcd.io/mpas-my-project-targets         1m    True   Source is not ready, artifact not found

NAME                                                                ROLE                                     AGE
rolebinding.rbac.authorization.k8s.io/leader-election-rolebinding   Role/mpas-project-leader-election-role   1m
rolebinding.rbac.authorization.k8s.io/mpas-my-project             ClusterRole/mpas-projects-clusterrole    1m
```

A GitHub repository should also exist at `<username>/my-project`.

## Testing

Run tests with make: `make test`

## Development

A `Tiltfile` is provided to make it easy to run the project controller locally. To run the project controller locally:

- Install [Tilt](https://tilt.dev)
- Clone the `git-controller` into the parent directory of the `mpas-project-controller`: `git clone https://github.com/open-component-model/git-controller ../git-controller`
- Export `GITHUB_USER`, `GITHUB_EMAIL`, and `GITHUB_TOKEN` environment variables. `repo` permissions are required for the `GITHUB_TOKEN`.
- Create a kind cluster: `kind create cluster`.
- Run `tilt up` from the `mpas-project-controller` directory.

Changes made during development will be automatically synced to the pod running in your local cluster via Tilt.

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/open-controller-model/mpas-project-controller/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright (20xx-)20xx SAP SE or an SAP affiliate company and <your-project> contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/open-component-model/mpas-project-controller).

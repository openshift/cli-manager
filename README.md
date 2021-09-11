# OpenShift CLI Manager
This is a Kubernetes controller intended to be used within an OpenShift cluster that adds additional functionality to the `oc` command to install and manage additional CLI tools and plugins in a disconnected environment.

## Motivation
In disconnected environments, it is more difficult to install and manage CLI tools. The existing mechanism within OpenShift for providing additional CLI tools (i.e. `ConsoleCLIDownload`) provides a local copy of `oc`, but for other tools it provides internet-facing links that are unreachable in disconnected environments.

## Design
This controller leverages images and registries for providing tools. This works by including any CLI tools desired into an image that is reachable from the cluster. This controller will pull this image, and extract the desired tool from the image's filesystem. Cluster administrators define `CLITool` custom resources which describe tool, the image:tag, and the path within the image to extract. Users can then download tools via this controller's API. Consuming this API is made more convenient with its integration into `oc`.

## Configuration
By default, this controller will watch `CLITool` resources in all namespaces. To restrict watching to a single namespace, set the `WATCH_NAMESPACE` environment variable.

## `CLITool` Specification
The spec has the following fields:
* `description`: User-friendly description of the tool
* `binaries`: List of binaries available for the tool based on operating system and CPU architecture each binary is compiled for
    * `os`: Operating system for the binary
    * `arch`: CPU architecture for the binary
    * `image`: Image name with tag to pull
    * `path`: The path to the binary within the image to extract
    * `imagePullSecret`: If authentication to the image registry is required, provide the name of the `dockercfg` Secret where the authentication information can be found

Example:
```yaml
apiVersion: config.openshift.io/v1
kind: CLITool
metadata:
  name: bash
  namespace: default
spec:
  description: just a test
  binaries:
  - os: linux
    arch: amd64
    image: redhat/ubi8-micro:latest
    path: /usr/bin/bash
```

### Available operating systems and CPU architectures
The most common are:
* `os`:
  * `darwin` (i.e. MacOS)
  * `linux`
  * `windows`
* `arch`:
  * `386`
  * `amd64`
  * `arm`
  * `arm64`

A complete list of all supported operating systems and architectures can be found here: https://github.com/golang/go/blob/master/src/go/build/syslist.go

## API Endpoints
### `GET/LIST /v1/tools/`
List available tools.

#### Request
No additional parameters at this time.

#### Response
Fields:
* `name`: Name of the tool
* `description`: Description of the tool
* `platforms`: List of suppported platforms with the following fields:
    * `os`: Name of the operating system
    * `arch`: Name of the CPU architecture

Example:
```json
{
  "items": [
    {
      "kind": "CLITool",
      "apiVersion": "config.openshift.io/v1",
      "metadata": {
        "name": "bash",
        "namespace": "default",
      },
      "spec": {
        "description": "just a test",
        "binaries": [
          {
            "os": "linux",
            "arch": "amd64",
            "image": "redhat/ubi8-micro:latest",
            "path": "/usr/bin/bash"
          }
        ]
      }
    }
  ]
}
```

### `GET /v1/tools/download/`
Download a tool.

#### Request
The following query parameters are required:
* `namespace`: Namespace for the CLITool resource
* `name`: Name of the CLITool resource
* `os`: Operating system for the binary
* `arch`: CPU architecture for the binary

Example:
```http
GET /v1/tools/download/?namespace=default&name=bash&os=linux&arch=amd64
```

#### Response
A successful response will contain the raw binary of the tool for the requested operating system and architecture.

## Limitations
The current design does not allow for versioning. Potential improvements:
* Add list of `versions` to `CLITool` and place the current `binaries` field under the new `versions` field
* Combine `os` and `arch` into `platform`, which is simply `os/arch`. This would allow for easier binary matching
* Managing locally installed versions is problematic, as we don't want to maintain a manifest of installed tools and versions
  * This could be worked around by updating `CLITool` to include `status.hashes[version/platform]=binary-hash`
  * The hash would be calculated by the controller the first time the tool is downloaded
  * `oc` could then perform the same hash on installed tools to match the hash stored in `CLITool.status.hashes` to determine locally installed version
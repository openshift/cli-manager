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
* `versions`: List of available versions of this tool, with latest version always listed last
* `binaries`: List of binaries available for this tool's version based on platform each binary is compiled for
    * `platform`: Operating system and CPU architecture for binary, in format `os/arch`
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
  versions:
  - v4.4.20
    binaries:
    - platform: linux/amd64
      image: redhat/ubi8-micro:latest
      path: /usr/bin/bash
```

### Available Platforms
The most common are:
  * `darwin/amd64` (i.e. MacOS)
  * `linux/amd64`
  * `windows/amd64`

A complete list of all supported platforms (i.e operating systems and architectures) can be found here: https://github.com/golang/go/blob/master/src/go/build/syslist.go

## API Endpoints
### `GET/LIST /v1/tools/`
List available tools.

#### Request
Fields:
* `platform`: (Optional) Limit results to tools that support the given platform

#### Response
Fields:
* `namespace`: Namespace of the CLITool resource
* `name`: Name of the CLITool resource
* `description`: Description of the tool
* `latestVersion`: Most recent version of the tool
  * `platforms`: List of suppported platforms with the following fields

Example:
```json
{
  "items": [
    {
      "namespace": "default",
      "name": "bash",
      "description": "just a test",
      "latestVersion": "v4.4.20",
      "platforms": [
        "linux/amd64"
      ]
    }
  ]
}
```

### `GET /v1/tools/info/`
Get version and binary information about a given tool.

#### Request
The following query parameters are required:
* `namespace`: Namespace of the CLITool resource
* `name`: Name of the CLITool resource
* `version`: (Optional) Only show info for specific version number or `latest` for most recent version

Example:
```http
GET /v1/tools/info/?namespace=default&name=bash
```

#### Response
Fields:
* `namespace`: Namespace for the CLITool resource
* `name`: Name of the CLITool resource
* `description`: Description of the tool
* `versions`: List of known version objects
  * `version`: Version name or number
  * `binaries`: List of binaries for various platforms for the given version
    * `platform`: Platform for the binary
    * `image`: Image containing the binaries for the given platform
    * `imagePullSecret`: The name of the Secret where image pull secrets can be found if the image registry requires credentials
    * `path`: Path within the given image where the binary for the given platform can be found
* `digests`: List of known version/platform combination hashes
  * `name`: Name of the hash, usually `version/platform`
  * `digest`: Text-representation of the hash digest
  * `calculated`: Unix timestamp of when the hash was calculated

Example:
```json
{
  "namespace": "default",
  "name": "bash",
  "description": "just a test",
  "versions": [
    {
      "version": "v4.4.20",
      "binaries": [
        {
          "platform": "linux/amd64",
          "image": "redhat/ubi8-micro:latest",
          "path": "/usr/bin/bash"
        }
      ]
    }
  ],
  "digests": [
    {
      "name": "v4.4.20/linux/amd64",
      "digest": "sha256:b379e9dff3...",
      "calculated": 1631382689
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
* `version`: Version name to download
* `platform`: Platform for the binary

Example:
```http
GET /v1/tools/download/?namespace=default&name=bash&version=v4.4.20&platform=linux/amd64
```

#### Response
A successful response will contain the raw binary of the tool for the requested operating system and architecture.
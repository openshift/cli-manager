# OpenShift CLI Manager
This is a Kubernetes controller intended to be used within an OpenShift cluster that adds additional functionality to the `oc` command to install and manage additional CLI tools and plugins in a disconnected environment.

## Motivation
In disconnected environments, it is more difficult to install and manage CLI tools. The existing mechanism within OpenShift for providing additional CLI tools (i.e. `ConsoleCLIDownload`) provides a local copy of `oc`, but for other tools it provides internet-facing links that are unreachable in disconnected environments.

## Design
This controller leverages images and registries for providing tools. This works by including any CLI tools desired into an image that is reachable from the cluster. This controller will pull this image, and extract the desired tool from the image's filesystem. Cluster administrators define `CLITool` custom resources which describe tool, the image:tag, and the path within the image to extract. Users can then download tools via this controller's API. Consuming this API is made more convenient with its integration into `oc`.

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
[
    {
        "name": "kubectl",
        "description": "Kubernetes cluster manager",
        "platforms": [
            {
                "os": "darwin",
                "arch": "amd64",
            },
            {
                "os": "linux",
                "arch": "amd64",
            }
        ]
    },
    {
        "name": "oc",
        "description": "OpenShift cluster manager",
        "platforms": [
            {
                "os": "darwin",
                "arch": "amd64",
            },
            {
                "os": "linux",
                "arch": "amd64",
            }
        ]
    }
]
```


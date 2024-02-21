# OpenShift CLI Manager
This is a Kubernetes controller intended to be used within an OpenShift cluster that adds additional functionality to the `oc` command to install and manage additional CLI plugins via `krew` in a disconnected environment.

## Status

### TECH PREVIEW
This project is currently under development, however, comments and feedback are always welcome!

## Motivation
In disconnected environments, it is more difficult to install and manage CLI plugins. The existing mechanism within OpenShift for providing additional CLI tools (i.e. `ConsoleCLIDownload`) provides a local copy of `oc`, but for other tools it provides internet-facing links that are unreachable in disconnected environments.

## Design
This controller leverages images and registries for providing `krew` plugins. This works by including any plugins desired into an image that is reachable from the cluster. This controller will pull this image, and extract the desired plugin from the image's filesystem. Cluster administrators define `Plugin` custom resources which describe the plugin, the image:tag, and the paths within the image to extract. Users can then download plugins via this controller's REST API or using Git's HTTP protocol (i.e `krew`). Consuming this API is made more convenient with `krew` integration into `oc`.

## Configuration
By default, this controller will watch `Plugin` resources in all namespaces. To restrict watching to a single namespace, set the `WATCH_NAMESPACE` environment variable.

## `Plugin` Specification
The spec has the following fields:
* `shortDescription`: Short, user-friendly description of the plugin
* `description`: Long, user-friendly description of the plugin
* `caveats`: Known caveats of using the plugin
* `homepage`: The homepage of the plugin
* `version`: The version of this plugin
* `platforms`: List of binaries available for this plugins based on platform each binary is compiled for
    * `platform`: Operating system and CPU architecture for binary, in format `os/arch` (i.e. `linux/amd64`)
    * `image`: Image name with tag to pull
    * `imagePullSecret`: If authentication to the image registry is required, provide the name of the `dockercfg` Secret where the authentication information can be found
    * `files`: List of files to pull from the image using absolute paths and where they should be installed relative to the installation's root directory
      * `from`: Absolute path to a file, directories and wildcards are not yet supported
      * `to`: Relative path to install the file, or `.` for installation root directory
    * `bin`: Name of the binary to execute

Example:
```yaml
apiVersion: config.openshift.io/v1
kind: Plugin
metadata:
  name: bash
  namespace: default
spec:
  shortDescription: just a test
  description: just a test
  version: v4.4.20
  platforms:
  - platform: linux/amd64
    image: redhat/ubi8-micro:latest
    files:
    - from: /usr/bin/bash
      to: "."
    bin: bash
```

## Client Configuration

In order to configure CLI Manager;

* oc (or kubectl) is installed
* Krew is installed. More details can be found https://krew.sigs.k8s.io/docs/user-guide/setup/install/
* Custom index provided by OpenShift CLI Manager is defined in Krew;
```sh
$ ROUTE=$(oc get route/cli-manager -n openshift-cli-manager -o=jsonpath='{.spec.host}')
$ CUSTOM_INDEX_NAME=ocp
$ oc krew add index $CUSTOM_INDEX_NAME https://$ROUTE/cli-manager
```

To search, install or remove a plugin;

```shell
$ oc krew search test
$ oc krew install $CUSTOM_INDEX_NAME/test
$ oc krew remove test
```

To update to the latest version of plugin;

```shell
$ oc krew update
```

### Available Platforms
The most common are:
  * `darwin/amd64` (i.e. MacOS)
  * `linux/amd64`
  * `windows/amd64`

A complete list of all supported platforms (i.e operating systems and architectures) can be found here: https://github.com/golang/go/blob/master/src/go/build/syslist.go

## API Endpoints

### `GET /v1/plugins/download/`
Download a plugin as a tar.gz archive.

#### Request
The following query parameters are required:
* `name`: Name of the Plugin resource
* `platform`: Platform for the binary

Example:
```http
GET /v1/plugins/download/?name=bash&platform=linux/amd64
```

#### Response
A successful response will contain the tar.gz archive of the plugin's files for the requested platform.

## OpenShift Self Signed Certificates

OpenShift serves endpoints with the CA bundles that is self-signed within the cluster. Certificate authority field in kubeconfig is used to interact with these components.
However, Krew does not provide a similar functionality to pass self-signed CA certificates explicitly as trusted to tackle unknown certificate errors. 
As a result, it is up to user to define these self-signed certificates as trusted in their local environments.

### Fedora

```sh
$ echo "$(oc config view --minify --flatten -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 --decode)" | sudo tee /etc/pki/ca-trust/source/anchors/cli.crt > /dev/null
$ sudo update-ca-trust
```
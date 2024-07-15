# About

A labelling agent for Swarm Node, provide node labels for running stacks/services on a given node.

## How it works

The agent is a simple service that listens for events on the Docker Engine API to detect when a new service is created or updated. When a new service is created or updated, the agent detech the node where the service is running and apply the labels to the node.

These labels can be use with constraints in the service definition to run the service on a specific node or as a dependency service to run on the same node.

**Example:**

We deploy a stack with two services, `ingress` and `frontend`. We want the `frontend` service to run on the same node as the `ingress` service. We can use the following labels:

```yaml
# stack.namespace=myapp
services:
  # The ingress service
  ingress:
    image: nginx
    ports:
      - "80:80"
      - "443:443"
    networks:
      ingress:
  # The frontend service should run on the same node as the ingress service
  frontend:
    image: nginx
    networks:
      ingress:
    deploy:
      placement:
        constraints:
          - node.labels.service.myapp_ingress == true
networks:
  ingress:
```

## Usage

```sh
usage: node-metadata-agent [<flags>]

Flags:
  --[no-]help                   Show context-sensitive help (also try --help-long and --help-man).
  --stack.namespace=""          Filter by stack namespace
  --refresh.interval=15s        refresh interval
  --[no-]preserve-service-name  Preserve service name
  --[no-]version                Prints current version.
  --[no-]short-version          Print just the version number.
```

## Deployment

Please see the [swarmlibs/swarmlibs](https://github.com/swarmlibs/swarmlibs) repository for examples on how to deploy the agent.

## License

Licensed under [MIT](./LICENSE).

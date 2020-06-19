## `docker-compose`

The `docker-compose` command is a wrapper around the real [`docker-compose`](https://github.com/docker/compose) that
provides compose-file relative resolution and composition of the `COMPOSE_FILE` environment variable  and `-f` flag
values.

Specifically this is a solution for issues [docker/compose#3874](https://github.com/docker/compose/issues/3874) and
[docker/compose#7546](https://github.com/docker/compose/issues/7546).

### Installation

Requires a working [Go installation](https://golang.org/dl/):

```
(cd $(mktemp -d) && go mod init mod && go get github.com/myitcv/docker-compose)
```

### Example

Setting the `COMPOSE_FILE` allows you to compose `docker-compose` files from different projects:

```
export COMPOSE_FILE=/path/to/project1/docker-compose.yml:/path/to/project2/docker-compose.yml
docker-compose up -d
```

The result is the "merge" of the `n` input files.

You can specify additional config files either by augmenting the `COMPOSE_FILE` environment variable, or by supplying
`-f` flag values. Assuming the value of `COMPOSE_FILE` from above:


```
docker-compose -f prod.yml config
```

would be the result of composing `/path/to/project1/docker-compose.yml`,
`/path/to/project2/docker-compose.yml` and `prod.yml`.

Resolution of a config file happens relative to its containing directory. For example,
`/path/to/project1/docker-compose.yml` would be resolved relative to `/path/to/project1`.

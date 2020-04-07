% toolbox-enter(1)

## NAME
toolbox\-enter - Enter a toolbox container for interactive use

## SYNOPSIS
**toolbox enter** [*options*] *CONTAINER*

## DESCRIPTION

Spawns an interactive shell inside a toolbox container. The container should
have been created using the `toolbox create` command. If there aren't any
containers, `toolbox enter` will offer to create one for you. When invoked with
the default parameters, and if there's only one container available, it will
fall back to it, even if it doesn't match the default name.

A toolbox container is an OCI container. Therefore, `toolbox enter` is
analogous to a `podman start` followed by a `podman exec`.

On Fedora the toolbox containers are tagged with the version of the OS that
corresponds to the content inside them. Their names are prefixed with the name
of the base image.

## OPTIONS ##

The following options are understood:

**--release** RELEASE, **-r** RELEASE

Enter a toolbox container for a different operating system RELEASE than the
host.

## EXAMPLES

### Enter a toolbox container using the default image matching the host OS

```
$ toolbox enter
```

### Enter a toolbox container using the default image for Fedora 30

```
$ toolbox enter --release f30
```

### Enter a custom toolbox container using a custom name

```
$ toolbox enter foo
```

## SEE ALSO

`buildah(1)`, `podman(1)`, `podman-exec(1)`, `podman-start(1)`

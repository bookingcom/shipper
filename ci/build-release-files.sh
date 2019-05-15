#!/bin/bash -e

function package {
    if [ "$#" -ne 1 ]; then
        echo "Version argument not specified"
        exit 1
    fi

    version="$1"
    test -n "$version" >/dev/null 2>&1 || { echo >&2 "given empty version"; exit 1; }
    echo "packaging $version"

    if [[ "$OS" == "Darwin" ]]; then
        TAR="gtar"
        SHA="shasum -a 256"
    elif [[ "$OS" == "Linux" ]]; then
        TAR="tar"
        SHA="sha256sum"
    else
        echo "Unsupported OS $operating system"
        exit 1
    fi

    command -v $TAR >/dev/null 2>&1 || { echo >&2 "I require '$TAR' but it's not installed. Aborting."; exit 1; }
    command -v $SHA >/dev/null 2>&1 || { echo >&2 "I require '$SHA' but it's not installed. Aborting."; exit 1; }
    command -v $GO  >/dev/null 2>&1 || { echo >&2 "I require '$GO' but it's not installed. Aborting."; exit 1; }

    if [[ -d "$BASE" ]]; then
        echo "cleaning $BASE"
        rm -rf ${BASE}/*
    fi

    GO="go"
    build_and_package "$version" linux   amd64
    build_and_package "$version" linux   386
    build_and_package "$version" darwin  amd64
    build_and_package "$version" darwin  386
    build_and_package "$version" windows amd64
    build_and_package "$version" windows 386
    build_and_package "$version" freebsd amd64
    build_and_package "$version" freebsd 386
    build_and_package "$version" openbsd amd64
    build_and_package "$version" openbsd 386
    build_and_package "$version" netbsd  amd64
    build_and_package "$version" netbsd  386
}

function create_deployment_yaml {
    sed s=\<IMAGE\>=bookingcom/shipper:$TRAVIS_TAG= kubernetes/shipper.deployment.yaml > ./kubernetes-deployment.yaml
}

function build_and_package {
    version="$1"
    os="$2"
    arch="$3"
    test -n "$version" >/dev/null 2>&1 || { echo >&2 "given empty version"; exit 1; }
    test -n "$os"      >/dev/null 2>&1 || { echo >&2 "given empty GOOS"; exit 1; }
    test -n "$arch"    >/dev/null 2>&1 || { echo >&2 "given empty GOARCH"; exit 1; }

    name="shipperctl-${version}.${os}-${arch}"
    tgz="${name}.tar.gz"

    echo ""
    echo "building $name ..."
    mkdir -p "$BASE/${name}"

    export CGO_ENABLED=0
    (export GOOS="$os" GOARCH="$arch"; $GO build -v -o "${BASE}/${name}/shipperctl" cmd/shipperctl/*.go)
    echo "... done"

    echo "packing $name into $tgz ..."
    $TAR -zcvf "${BASE}/${tgz}" -C "$BASE" "$name"
    echo "... done"

    echo "computing checksum for $tgz ..."
    (cd $BASE; $SHA "$tgz" >> sha256sums.txt)
    echo "... done"
}

if [ "$TRAVIS_BRANCH" == "$TRAVIS_TAG" ]; then
    BASE="release-files"
    OS="`uname`"
    package "$TRAVIS_TAG"
    create_deployment_yaml
fi

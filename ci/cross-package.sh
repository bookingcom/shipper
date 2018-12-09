#!/bin/bash -e
#set -x

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

function package {
    if [ "$#" -ne 1 ]; then
        echo "use cross-package.sh package <version>"
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
        echo "Unsupported OS $OS"
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

function upload_asset {
    repo="$1"
    test -n "$repo" >/dev/null 2>&1 || { echo >&2 "given empty repo"; exit 1; }

    release="$2"
    test -n "$release" >/dev/null 2>&1 || { echo >&2 "given empty release"; exit 1; }

    file="$3"
    test -n "$file" >/dev/null 2>&1 || { echo >&2 "given empty file"; exit 1; }

    name=`basename $file`
    test -n "$name" >/dev/null 2>&1 || { echo >&2 "given empty name"; exit 1; }

    test -n "$USER" >/dev/null 2>&1 || { echo >&2 "I need USER envvar which match GitHub account name"; exit 1; }
    test -n "$GITHUB_TOKEN" >/dev/null 2>&1 || { echo >&2 "I need GITHUB_TOKEN envvar which should contain personal access token. More at https://developer.github.com/v3/auth/#via-oauth-tokens"; exit 1; }

    case $name in
        *.tar.gz)
            content_type="application/gzip"
            ;;
        *.txt)
            content_type="text/plain"
            ;;
        *)
            echo "unsuppored file format $name for uploading to github"
            exit 1
    esac


    CURL="curl"
    command -v $CURL >/dev/null 2>&1 || { echo >&2 "I require '$CURL' but it's not installed. Aborting."; exit 1; }

    echo ""
    echo "uploading $name to $repo ..."
    STATUS_CODE=`$CURL --basic --user "$USER:$GITHUB_TOKEN" --max-time 60 --silent --output /dev/stderr --write-out "%{http_code}" -H "Content-Type: $content_type" --data-binary "@${file}" "https://uploads.github.com/repos/${repo}/releases/${release}/assets?name=$name"`

    if [[ "$STATUS_CODE" == "422" ]]; then
        echo ""
        echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
        echo "!!! it seems that $name is already uploaded, please remove and rerun if want to re-upload !!!"
        echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    elif [[ "$STATUS_CODE" == "201" ]]; then
        echo ""
        echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
        echo "!!! $name is uploaded !!!"
        echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    else
        echo "... failed"
        exit 1
    fi
}

function upload {
    if [ "$#" -lt 1 ]; then
        echo "use cross-package.sh upload <repo> <release-id>"
        exit 1
    fi

    repo="$1"
    test -n "$repo" >/dev/null 2>&1 || {
        echo >&2 "given empty repo!";
        echo >&2 "likely it should be bookingcom/shipper"
        exit 1;
    }

    release="$2"
    test -n "$release" >/dev/null 2>&1 || {
        echo >&2 "given empty release id!";
        echo >&2 "use following command to get latest release id:"
        echo >&2 "curl https://api.github.com/repos/${repo}/releases 2>/dev/null | jq '.[0].id'"
        exit 1;
    }

    for file in `ls ${BASE}/*.tar.gz ${BASE}/sha256sums.txt 2>/dev/null`; do
        upload_asset "$repo" "$release" "$file"
    done
}

COMMAND=$1
BASE="cross-package"
OS="`uname`"

case $COMMAND in
    "package")
        package ${@:2}
        ;;
    "upload")
        upload ${@:2}
        ;;
    *)
        echo "Unknown command $COMMAND"
        exit 1
        ;;
esac

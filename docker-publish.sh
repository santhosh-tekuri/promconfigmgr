#!/usr/bin/env bash

set -e
if [[ $# -ne 1 ]]; then
    echo "Usage: docker-publish.sh TAG"
    exit 1
fi
name=santhoshkt/promconfigmgr
image=$name:$1

if ! docker manifest >/dev/null 2>&1 ; then
    docker manifest
fi

cat <<EOF > Dockerfile
FROM scratch
COPY promconfigmgr /
ENTRYPOINT ["/promconfigmgr"]
EOF
trap "rm -f Dockerfile promconfigmgr" EXIT

archs=(amd64 arm arm64 ppc64le s390x)
declare -a images
for arch in "${archs[@]}"; do
    echo bulding ${image}-${arch} ----------------------
    rm -f promconfigmgr
    docker run --rm -v "$PWD":/promconfigmgr -w /promconfigmgr -e GOARCH=${arch} -e CGO_ENABLED=0 golang:1.14.4 go build -a
    docker build -t ${image}-${arch} .
    docker push ${image}-${arch}
    images+=(${image}-${arch})
done

function deploy_manifest() {
  manifest=$1
  echo buildings manifest $manifest ----------------------
  docker manifest create ${manifest} ${images[@]}
  for arch in "${archs[@]}"; do
    docker manifest annotate ${manifest} ${image}-${arch} --os linux --arch ${arch}
  done
  docker manifest inspect ${manifest}
  docker manifest push --purge ${manifest}
}

deploy_manifest ${image}
deploy_manifest $name:${1:0:3}
deploy_manifest $name:${1:0:1}
deploy_manifest $name:latest


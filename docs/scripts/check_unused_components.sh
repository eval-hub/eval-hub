#!/bin/bash

# set -x

grep_opts="-r"

for dir in ./docs/src/components/response ./docs/src/components/examples ./docs/src/components/schemas ; do
  FILES=""
  for file in ${dir}/*.yaml; do
    f=$(basename -- ${file})
    FILES="${FILES} ${file}"
    if ! grep ${grep_opts} -e "/${f}" ./docs/src > /dev/null ; then
      echo "Found no occurences of ${file}"
      exit 1
    fi
  done
  # echo "Checked ${FILES}"
  echo "Checked all files in ${dir}"
done

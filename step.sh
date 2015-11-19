#!/bin/bash

set -e

#
# Input validation
if [[ -z "${xamarin_solution}" ]] ; then
  echo "Missing required input: xamarin_solution"
  exit 1
fi

if [[ ! -e "${xamarin_solution}" ]] ; then
  echo "File not exist at: ${xamarin_solution}"
  exit 1
fi

#
# Print configs
echo
echo '========== Configs =========='
echo " * xamarin_solution: ${xamarin_solution}"

#
# Nuget restore
nuget="/Library/Frameworks/Mono.framework/Versions/Current/bin/nuget"

echo
echo "${nuget}" restore "${xamarin_solution}"
"${nuget}" restore "${xamarin_solution}"

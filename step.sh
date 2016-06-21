#!/bin/bash

set -e

#=======================================
# Functions
#=======================================

RESTORE='\033[0m'
RED='\033[00;31m'
YELLOW='\033[00;33m'
BLUE='\033[00;34m'
GREEN='\033[00;32m'

function color_echo {
	color=$1
	msg=$2
	echo -e "${color}${msg}${RESTORE}"
}

function echo_fail {
	msg=$1
	echo
	color_echo "${RED}" "${msg}"
	exit 1
}

function echo_warn {
	msg=$1
	color_echo "${YELLOW}" "${msg}"
}

function echo_info {
	msg=$1
	echo
	color_echo "${BLUE}" "${msg}"
}

function echo_details {
	msg=$1
	echo "  ${msg}"
}

function echo_done {
	msg=$1
	color_echo "${GREEN}" "  ${msg}"
}

function validate_required_file_input {
	key=$1
	value=$2
	if [ -z "${value}" ] ; then
		echo_fail "[!] Missing required input: ${key}"
	fi

  if [[ ! -e "${value}" ]] ; then
    echo_fail "[!] File not exist at: ${value}"
  fi
}

function print_and_run {
  cmd="$1"
  echo_details "${cmd}"
  eval "${cmd}"
}

#=======================================
# Main
#=======================================

# Parameters
echo_info "Configs:"
echo_details "* xamarin_solution: $xamarin_solution"
echo_details "* nuget_version: $nuget_version"

validate_required_file_input "xamarin_solution" $xamarin_solution

# Current nuget version
nuget="/Library/Frameworks/Mono.framework/Versions/Current/bin/nuget"

help_out=$(nuget help)
regex="NuGet Version: ([0-9]+.[0-9]+.[0-9]+).*"
current_nuget_version=""

if [[ $help_out =~ $regex ]] ; then
  current_nuget_version="${BASH_REMATCH[1]}"

  echo_details "* current_nuget_version: $current_nuget_version"
fi

# Install NuGet version
if [[ -n "$nuget_version" ]] && [ "$current_nuget_version" != "$nuget_version" ] ; then
  echo_info "Downloading NuGet version: $nuget_version"

  if [[ "$nuget_version" == "latest" ]] ; then
    print_and_run "sudo $nuget update -self"
  else
    nuget_url="https://dist.nuget.org/win-x86-commandline/v$nuget_version/nuget.exe"
    temp_path=$(mktemp -d)
    nuget_path="$temp_path/nuget.exe"

    print_and_run "curl $nuget_url -o $nuget_path -s"

    nuget="mono $nuget_path"
  fi
fi

# NuGet restore
echo_info "NuGet restore"

print_and_run "$nuget restore $xamarin_solution"

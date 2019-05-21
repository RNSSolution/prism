#!/usr/bin/env sh

if [ "$1" = "clean" ]; then
	echo "Removing configuration files..."
	rm -rf /prism/node*
	echo "Done"
	exit 0;
fi

##
## Input parameters
##
BINARY=/prism/${BINARY:-prism}
ID=${ID:-0}
LOG=prism.log

[ -f "/prism/docker-run-before.sh" ] && source /prism/docker-run-before.sh

##
## Assert linux binary
##
if ! [ -f "${BINARY}" ]; then
	echo "The binary $(basename "${BINARY}") cannot be found. Please add the binary to the shared folder. Please use the BINARY environment variable if the name of the binary is not 'tendermint' E.g.: -e BINARY=tendermint_my_test_version"
	exit 1
fi
BINARY_CHECK="$(file "$BINARY" | grep 'ELF 64-bit LSB executable, x86-64')"
if [ -z "${BINARY_CHECK}" ]; then
	echo "Binary needs to be OS linux, ARCH amd64"
	exit 1
fi

##
## Run binary with all parameters
##
export TMHOME="/prism/node${ID}"
mkdir -p "${TMHOME}"

umask 022
"$BINARY" $COMMAND_ARGS "$@" $SUBCOMMAND_ARGS | tee "${TMHOME}/${LOG}"

chmod 777 -R /prism

[ -f "/prism/docker-run-after.sh" ] && source /prism/docker-run-after.sh

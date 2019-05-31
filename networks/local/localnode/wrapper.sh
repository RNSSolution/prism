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
echo "WRAPPER DEBUG: ID=${ID} LOG=${LOG} COMMAND_ARGS=${COMMAND_ARGS} SUBCOMMAND_ARGS=${SUBCOMMAND_ARGS}"
BINARY=/prism/${BINARY:-prism}
ID=${ID:-0}
LOG=$(printf "prism-%02d.log" ${ID})

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
export PRISM_HOME="/prism/node${ID}"
mkdir -p "${PRISM_HOME}"

umask 022
# Filter away debug lines from the screen
"$BINARY" $COMMAND_ARGS "$@" $SUBCOMMAND_ARGS | tee "${PRISM_HOME}/${LOG}" | awk '/^D/ {debug=1;next;} /^[I|E]/ {debug=0;} { if (!debug) {print;} }'

chmod 777 -R /prism

[ -f "/prism/docker-run-after.sh" ] && source /prism/docker-run-after.sh

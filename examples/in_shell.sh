#!/bin/bash

# For those who have used dd, you have probably been annoyed by its tendencies
# to output.  Notice the lack of redirectors.
#
# NOTE:  Only the second example should produce output
alarm -squelch dd if=/dev/urandom  of=/dev/zero bs=1024 count=1

# And if we're doing something that sometimes (or always) fails:
alarm -squelch bash -c "echo This command failed; false"

# And if we're doing something that sometimes (or always) fails:
alarm -squelch bash -c "echo This command succeeds, you will not see this; true"

# And for the pesky commands that sometimes just hang:
alarm -squelch -time="5,10:9" sleep 15

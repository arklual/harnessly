#!/bin/sh
# Renders the scene frame by frame; the log line is `frame N/3000`.
i=0
while [ "$i" -lt 3000 ]; do
  i=$((i + 1))
  echo "frame $i/3000"
  sleep 1
done
echo "RENDER DONE"

#!/bin/bash

# Usage:
# paste
# paste < tmp.log
# cat tmp.log | paste

# shellcheck disable=SC1091
source "$HOME/.profile"

TOKEN="$FOH_SERVER_AUTH"
URL="https://i.l1v.in"
CDN_URL="https://cdn.l1v.in" # This is a reverse proxy to $URL/media/
APP_NAME="cdn.l1v.in"

# Set default filename and path
filename="$(date +"paste-%s.txt")"
filepath="$HOME/.cache/$filename"

printf 'Type your paste and press \u001b[31mCtrl D\u001b[0m when finished\n'

cat - > "$filepath"
printf 'Uploading...\n'

# Upload the screenshot
curl -s -X POST -H "Auth: $TOKEN" "$URL/public/media/$filename" -F "file=@$filepath"

# Copy the screenshot URL to clipboard
printf '%s/%s' "$CDN_URL" "$filename" | xclip -sel clip
echo "Uploaded $CDN_URL/$filename"

# Send notification after copying to clipboard
notify-send "Uploaded paste" "$filename" --icon=clipboard --app-name="$APP_NAME"

# Remove temporary file
rm "$filepath"

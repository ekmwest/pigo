pigo

## Install

windows (powershell):

    irm https://raw.githubusercontent.com/ekmwest/pigo/main/scripts/install.ps1 | iex

macos and linux:

    curl -fsSL https://raw.githubusercontent.com/ekmwest/pigo/main/scripts/install.sh | sh

## Update

    pigo update

## Share

Not part of pigo. You can use cloudflared directly, including header rewrite, like so:

    cloudflared tunnel --url localhost:5000 --http-host-header localhost:5000

You can also add a shortcut in your $PROFILE

    function share($port) { cloudflared tunnel --url "localhost:$port" --http-host-header "localhost:$port" }

And call it from PowerShell like so:

    share 5000

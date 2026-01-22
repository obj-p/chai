.PHONY: all clean generate build distribute serve certs

all: clean generate

clean:
	@rm -rf Chai.xcodeproj
	@rm -rf dist

generate:
	@xcodegen

build:
	@bundle exec fastlane build

distribute:
	@bundle exec fastlane distribute

certs:
	@mkdir -p ~/.local/share/tailscale-certs
	@tailscale cert --cert-file ~/.local/share/tailscale-certs/${CHAI_TAILSCALE_DOMAIN}.crt --key-file ~/.local/share/tailscale-certs/${CHAI_TAILSCALE_DOMAIN}.key ${CHAI_TAILSCALE_DOMAIN}

serve:
	@caddy run --config Caddyfile

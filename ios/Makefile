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
	@mkdir -p ~/.local/share/caddy-certs
	@mkcert -cert-file ~/.local/share/caddy-certs/${CHAI_DISTRIBUTION_DOMAIN}.crt -key-file ~/.local/share/caddy-certs/${CHAI_DISTRIBUTION_DOMAIN}.key ${CHAI_DISTRIBUTION_DOMAIN}

serve:
	@caddy run --config Caddyfile

build:
	@sh -c "'$(CURDIR)/build.sh'"

push:
	@sh -c "'$(CURDIR)/push.sh'"

build_develop:
	docker build -t "ColorPlatform/prism:develop" -f Dockerfile.develop .

build_testing:
	docker build --tag ColorPlatform/testing -f ./Dockerfile.testing .

push_develop:
	docker push "ColorPlatform/prism:develop"

.PHONY: build build_develop push push_develop

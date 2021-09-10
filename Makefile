test:
	for x in *.sh; do bash -n $$x; done
.PHONY: test

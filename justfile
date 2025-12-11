dev:
    fd 'go|templ' | entr -r bash -c 'just templ && go build -o ./pyramid-sebastix && godotenv ./pyramid-sebastix'

build: templ
    CC=musl-gcc go build -ldflags='-linkmode external -extldflags "-static"' -o ./pyramid-sebastix

templ:
    templ generate

deploy target: build
    ssh root@{{target}} 'systemctl stop pyramid';
    scp pyramid-sebastix {{target}}:pyramid/pyramid
    ssh root@{{target}} 'systemctl start pyramid'

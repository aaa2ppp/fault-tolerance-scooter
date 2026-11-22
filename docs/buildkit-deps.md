## Обновление зависимостей для BuildKit

Последние версии, которые не ломают сборку scooterctl на v1.26.1 

```sh
(cd scooterctl && go get github.com/moby/buildkit@v0.31.0)
# go: upgraded github.com/Microsoft/go-winio v0.4.14 => 0.6.2
# go: upgraded github.com/coreos/go-systemd/v22 v22.5.0 => 22.7.0
# go: upgraded github.com/docker/cli v28.3.3+incompatible => 29.5.3+incompatible
# go: upgraded github.com/gofrs/flock v0.12.1 => 0.13.0
# go: added github.com/moby/buildkit v0.31.0
# go: upgraded go.yaml.in/yaml/v2 v2.4.3 => 2.4.4
# go: upgraded golang.org/x/time v0.9.0 => 0.15.0
# go: upgraded k8s.io/klog/v2 v2.130.1 => 2.140.0

(cd scooterctl && go get github.com/moby/moby/client@v0.4.0)
# go: upgraded github.com/docker/go-connections v0.5.0 => 0.6.0
# go: added github.com/moby/moby/api v1.54.1
# go: added github.com/moby/moby/client v0.4.0

(cd scooterctl && go mod tidy)
```

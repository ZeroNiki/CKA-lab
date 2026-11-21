# CKA Lab

Локальный тренажёр к экзамену CKA. Уровни с теорией-допуском и практикой в настоящем kubeadm-кластере (Kubernetes v1.37, Vagrant + libvirt или VirtualBox), автоматическая проверка заданий, койны и перерывы.

## Требования

- Хост x86_64 (Linux или macOS на Intel): скрипты ставят amd64-бинарники в VM.
- Около 8 GB свободной RAM: `controlplane` 4 GB, `node01` и `node02` по 2 GB.
- Один из провайдеров:

| Провайдер | Vagrantfile | Что нужно на хосте |
|---|---|---|
| `libvirt` (по умолчанию) | `Vagrantfile` | libvirt/KVM, плагин `vagrant-libvirt`, `rsync` |
| `virtualbox` | `Vagrantfile.virtualbox` | VirtualBox 7.x |

- Go, kubectl, shellcheck и vagrant берутся из `nix develop` (Nix с flakes), `./lab` заходит в него сам. Как обойтись без Nix, см. [Без Nix](#без-nix).

> [!NOTE]
> На Linux VirtualBox может не запустить VM с ошибкой `VERR_VMX_IN_VMX_ROOT_MODE`, пока загружены модули KVM. Тогда выгрузи их (`sudo modprobe -r kvm_intel kvm` или `kvm_amd kvm`) или используй libvirt.

## Без Nix

Если `go` уже есть в `PATH`, `./lab` в `nix develop` не заходит. Тогда на хосте нужны только Go 1.26+, `vagrant` и `curl`: к нодам `./lab` подключается через SSH-библиотеку Go, а `kubectl` запускается внутри VM. Проще всего начать с VirtualBox.

### Ubuntu / Debian

```bash
# Vagrant из репозитория HashiCorp
wget -O - https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" \
  | sudo tee /etc/apt/sources.list.d/hashicorp.list
sudo apt update
sudo apt install -y vagrant virtualbox golang-go curl
```

### Fedora

```bash
sudo dnf install -y dnf-plugins-core
sudo dnf config-manager addrepo --from-repofile=https://rpm.releases.hashicorp.com/fedora/hashicorp.repo
sudo dnf install -y vagrant golang curl
```

VirtualBox в Fedora не входит: его ставят из [RPM Fusion](https://rpmfusion.org/) (`VirtualBox`) или с [virtualbox.org](https://www.virtualbox.org/wiki/Linux_Downloads).

### Запуск

```bash
LAB_PROVIDER=virtualbox ./lab up
```

> [!WARNING]
> Проект требует Go 1.26+ (`go.mod`). Если `go version` показывает версию старше 1.21, Go сам скачает нужную, но некоторые дистрибутивы это запрещают (`GOTOOLCHAIN=local`), и сборка падает с `go.mod requires go >= 1.26.0`. Тогда разреши загрузку один раз: `go env -w GOTOOLCHAIN=auto`. Если Go старше 1.21, поставь свежий с <https://go.dev/dl/>.

> [!TIP]
> Хочешь libvirt вместо VirtualBox: поставь плагин по [инструкции vagrant-libvirt](https://vagrant-libvirt.github.io/vagrant-libvirt/installation.html), добавь себя в группу `libvirt` и запускай `./lab up` без `LAB_PROVIDER`.

## Первый запуск

```bash
./lab up              # создать 3 VM, kubeadm, Calico, аддоны; запустить тренажёр
./lab snapshot-base   # один раз: snapshot "clean" всех нод, нужен для заданий, ломающих кластер
```

UI открывается на <http://127.0.0.1:8080>.

### VirtualBox

```bash
LAB_PROVIDER=virtualbox ./lab up
./lab snapshot-base
```

`./lab` запоминает провайдер в `.lab/provider`, поэтому `./lab ssh`, `./lab reset` и тренажёр работают с теми же VM без `LAB_PROVIDER`.

Чтобы сменить провайдер, останови стенд (`./lab down`), удали его VM (`vagrant destroy -f` для libvirt, `VAGRANT_VAGRANTFILE=Vagrantfile.virtualbox vagrant destroy -f` для VirtualBox) и выполни `rm .lab/provider`.

Напрямую через vagrant: `VAGRANT_VAGRANTFILE=Vagrantfile.virtualbox vagrant status`.

## Обычная сессия

```bash
./lab up          # поднять VM (если остановлены) и тренажёр
./lab ssh         # работать на controlplane: kubectl, helm, etcdctl, alias k
./lab ssh node01  # для заданий на worker-ноде
./lab down        # остановить тренажёр и VM, освободить ресурсы ноутбука
```

| Команда | Что делает |
|---|---|
| `./lab up` | `vagrant up` при необходимости, аддоны, kubeconfig в `.lab/`, (пере)запуск сервера |
| `./lab down` | останавливает сервер и `vagrant halt` |
| `./lab ssh [node]` | shell на ноде, по умолчанию `controlplane` |
| `./lab snapshot-base` | `vagrant snapshot save clean` для всех нод |
| `./lab reset` | откатить все ноды к `clean` |
| `./lab selftest ...` | проверить задания эталонными решениями (см. ниже) |

## Правила игры

- **Уровень**: 10 вопросов теории, затем 7 практических заданий. Все уровни открыты сразу.
- **Теория-допуск**: можно ошибиться 1 раз. На второй ошибке блок заканчивается, вопросы и варианты перемешиваются заново. Сданная теория запоминается.
- **Практика**: `Start` готовит задание (свежий namespace `lab-*` и стартовые объекты). Решаешь в терминале, затем `Check`.
- **Койны**: medium 20, hard 30. Повторное решение даёт 30%, бонус за уровень 50 (один раз). `Show solution` до решения обнуляет награду за это решение. Задание всё равно нужно решить.
- **Режимы**: `Mixed` (вопросы и задания всех тем вперемешку) и `Practice only`. Койны там начисляются, но прохождение уровня засчитывается только в режиме уровней.
- **Задания со snapshot** ломают кластер (etcd, kubelet, API server). После `Check` (pass) или `Give up` все ноды откатываются к `clean`. В Mixed и Practice only они включаются чекбоксом.
- **Магазин**: перерывы на 5, 15 и 30 минут. По окончании перерыва браузер показывает уведомление.

Числа меняются в `config.yaml`.

## Как добавить задание

Каждое задание лежит в отдельном YAML-файле в `content/levels/<уровень>/tasks/`, порядок определяется именем файла. В уровне должно быть ровно 7 заданий и 10 вопросов в `theory.yaml`. Если контент невалиден, сервер не стартует и пишет, в каком файле и поле ошибка.

```yaml
id: workloads-08-example          # уникальный
title: Short title
difficulty: hard                  # medium | hard (easy есть, но в уровнях не используется)
ns: lab-example                   # namespace задания, пересоздаётся при Start; обязательно "lab-"
requiresSnapshot: false           # true, если задание ломает кластер
statement: |
  What must be true at the end. No commands, no hints.
setup:                            # необязательно
  manifests:                      # применяются в ns задания (cluster-scoped тоже можно)
    - |
      apiVersion: v1
      kind: ConfigMap
      metadata: {name: seed}
  commands:                       # bash на ноде от пользователя vagrant (root через sudo)
    - node: node01                # по умолчанию controlplane
      run: sudo rm -f /etc/kubernetes/manifests/x.yaml
checks: [...]                     # см. ниже
docs: https://kubernetes.io/docs/...   # показывается после решения
solution: |                       # показывается после решения
  kubectl ...
solutionNode: controlplane        # где выполнять solutionScript
solutionScript: |                 # эталонное решение для ./lab selftest
  set -euo pipefail
  kubectl ...
```

### Типы проверок

Проверки задания выполняются все, даже если первая уже упала. `retry: 60s` повторяет проверку каждые 2 секунды, пока она не пройдёт. `timeout` задаёт лимит одной попытки, по умолчанию 10 секунд.

```yaml
# resource: объект(ы) по имени или labelSelector + JSONPath
- type: resource
  description: Deployment api has 3 replicas
  target: {apiVersion: apps/v1, kind: Deployment, name: api}   # namespace: по умолчанию ns задания
  assert:
    - {path: "{.spec.replicas}", op: eq, value: "3"}
# операторы: eq ne contains matches gte lte exists notExists count (count без path)
# при labelSelector assertion должен выполняться на каждом объекте

# podsReady: все Pod по селектору Ready, не меньше min
- type: podsReady
  description: api Pods are Ready
  selector: app=api
  min: 2
  retry: 120s

# canI: SubjectAccessReview (как kubectl auth can-i)
- type: canI
  description: deployer can create deployments
  subject: {serviceAccount: "lab-rbac:deployer"}   # или user / group
  verb: create
  resource: deployments.apps
  allowed: true                                    # по умолчанию true

# command: bash на ноде, проверка exit code и stdout
- type: command
  description: etcd backup exists
  node: controlplane
  run: sudo test -f /opt/lab/etcd-backup.db
  exitCode: 0
  stdout: {op: eq, value: "..."}                    # необязательно
```

### Проверка нового задания

```bash
./lab selftest workloads-08-example   # одно задание
./lab selftest --level workloads      # уровень
./lab selftest --all                  # всё; задания со snapshot идут последними
```

Для каждого задания selftest выполняет `setup` и проверки (ожидается fail: задание не должно решаться само), затем `solutionScript` и проверки ещё раз (ожидается pass). Задания со snapshot после этого откатываются.

## Разработка

```bash
nix develop
go test -race ./...
shellcheck lab scripts/*.sh
go run ./cmd/cka-lab serve -content internal/content/testdata/content   # UI без кластера
```

## Лицензия

MIT, см. [LICENSE](LICENSE). Сторонний код и источники идей перечислены в [NOTICE](NOTICE).

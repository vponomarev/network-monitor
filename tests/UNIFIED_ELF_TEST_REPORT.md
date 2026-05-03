# Тест единого .o файла eBPF на всех хостах

**Дата:** 3 мая 2026  
**Цель:** Проверить, что один и тот же скомпилированный `.o` файл работает на всех тестовых хостах с разными версиями ядра.

---

## Результаты

### ✅ УСПЕШНО: Единый .o файл работает на всех хостах

| Хост | ОС | Ядро | MD5SUM | Загрузка eBPF |
|------|----|------|--------|---------------|
| 192.168.5.217 | Ubuntu 22.04 | 5.15.0-177 | `af533b8fa6f66da804e7bc880d5e8644` | ✅ SUCCESS |
| 192.168.5.214 | Debian 13 | 6.12.85 | `af533b8fa6f66da804e7bc880d5e8644` | ✅ SUCCESS |
| 192.168.5.193 | Debian 12 | 6.1.0-45 | `af533b8fa6f66da804e7bc880d5e8644` | ✅ SUCCESS |
| 192.168.5.99 | Debian 12 + Proxmox | 6.8.12-20-pve | `af533b8fa6f66da804e7bc880d5e8644` | ✅ SUCCESS |

**Все 4 хоста загрузили один и тот же .o файл без ошибок!**

---

## Детали теста

### Компиляция

```bash
# Скомпилировано на: Debian 13 (192.168.5.214)
cd /tmp/conntrack-bpf/bpf
make all

# Результат:
#   CLANG   conntrack.bpf.o
#   STRIP   conntrack.bpf.o
# -rw-r--r-- 1 root root 21120 conntrack.bpf.o
```

### eBPF программы в .o файле

```
Programs: [tcp_connect inet_csk_accept tcp_close]
Maps: [events connections .rodata]
```

### CO-RE (Compile Once, Run Everywhere)

Единый .o файл работает на разных ядрах благодаря:

1. **BTF (BPF Type Format)** — информация о типах ядра читается из `/sys/kernel/btf/vmlinux` на целевом хосте
2. **eBPF CO-RE** — relocation выполняется при загрузке, а не при компиляции
3. **vmlinux.h** — содержит типы ядра для компиляции, но финальные адреса разрешаются в рантайме

---

## Проблемы и решения

### Проблема 1: Go 1.18/1.19 на Ubuntu 22.04 и Proxmox

**Симптом:**
```
package maps is not in GOROOT
package slices is not in GOROOT
```

**Причина:** Cilium eBPF v0.15.0 требует Go 1.21+ (пакеты `maps`/`slices` появились в Go 1.21)

**Решение:** Установлен Go 1.21.0 на оба хоста:
```bash
wget https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
```

---

## Выводы

1. **Единый .o файл работает на всех хостах** — компиляция на macOS не требуется
2. **Рекомендуемый подход:** компилировать .o файл на одном из Linux хостов (например, Debian 13)
3. **BTF обязателен** — все тестовые хосты имеют `/sys/kernel/btf/vmlinux`
4. **Проблема была не в .o файле** — компиляция на macOS могла иметь проблемы с совместимостью, но основной проблемой были версии Go для тестирования

---

## Рекомендации для CI/CD

```bash
# 1. Собирать .o файл на Linux (не на macOS)
# 2. Использовать единый .o файл для всех дистрибутивов
# 3. Убедиться, что Go >= 1.21 для совместимости с cilium/ebpf v0.15.0

# Пример скрипта сборки:
ssh build-host@linux "cd /path/to/bpf && make all"
scp build-host@linux:/path/to/bpf/conntrack.bpf.o ./internal/conntrack/bpf/
```

---

## Команды для воспроизведения

```bash
# 1. Скомпилировать на Debian 13
ssh root@192.168.5.214 "cd /tmp/conntrack-bpf/bpf && make clean && make all"

# 2. Скопировать на все хосты
for host in 192.168.5.217 192.168.5.214 192.168.5.193 192.168.5.99; do
    scp /tmp/conntrack-bpf/bpf/conntrack.bpf.o root@$host:/tmp/conntrack.bpf.o
done

# 3. Проверить MD5SUM
for host in 192.168.5.217 192.168.5.214 192.168.5.193 192.168.5.99; do
    ssh root@$host "md5sum /tmp/conntrack.bpf.o"
done

# 4. Запустить тест загрузки
for host in 192.168.5.217 192.168.5.214 192.168.5.193 192.168.5.99; do
    ssh root@$host "export PATH=/usr/local/go/bin:\$PATH && cd /tmp/verify-elf && go run verify_elf.go ./conntrack.bpf.o"
done
```

#!/usr/bin/env bash
#
# Script для запуска тестов Network Monitor на удалённой Linux машине
# Использование: ./scripts/run-remote-tests.sh [host1] [host2] ...
#
# Примеры:
#   ./scripts/run-remote-tests.sh                    # Хосты по умолчанию
#   ./scripts/run-remote-tests.sh 192.168.5.214      # Один хост
#   ./scripts/run-remote-tests.sh 192.168.5.214 192.168.5.193  # Несколько хостов
#

set -euo pipefail

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Хосты по умолчанию (можно переопределить через аргументы)
DEFAULT_HOSTS=("192.168.5.214" "192.168.5.193" "192.168.5.217" "192.168.5.99")
REMOTE_USER="${REMOTE_USER:-root}"
REMOTE_DIR="/tmp/network-monitor-tests"
LOCAL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_VERSION="${GO_VERSION:-1.24}"

# Флаги
RUN_UNIT_TESTS="${RUN_UNIT_TESTS:-true}"
RUN_INTEGRATION_TESTS="${RUN_INTEGRATION_TESTS:-true}"
RUN_E2E_TESTS="${RUN_E2E_TESTS:-false}"
GENERATE_COVERAGE="${GENERATE_COVERAGE:-true}"
CLEANUP="${CLEANUP:-true}"

# Логирование
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Проверка зависимостей
check_dependencies() {
    log_info "Проверка зависимостей..."
    
    local deps=("ssh" "scp" "rsync")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Требуется $dep, но он не установлен"
            exit 1
        fi
    done
    
    log_success "Все зависимости установлены"
}

# Получение списка хостов
get_hosts() {
    if [ $# -gt 0 ]; then
        echo "$@"
    else
        echo "${DEFAULT_HOSTS[@]}"
    fi
}

# Проверка доступности хоста
check_host() {
    local host="$1"
    log_info "Проверка доступности хоста $host..."
    
    if ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "echo 'OK'" &> /dev/null; then
        log_success "Хост $host доступен"
        return 0
    else
        log_error "Хост $host недоступен"
        return 1
    fi
}

# Получение информации о хосте
get_host_info() {
    local host="$1"
    log_info "Получение информации о хосте $host..."
    
    ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        echo '=== System Information ==='
        echo \"Hostname: \$(hostname)\"
        echo \"OS: \$(cat /etc/os-release | grep PRETTY_NAME | cut -d'\"' -f2)\"
        echo \"Kernel: \$(uname -r)\"
        echo \"Go: \$(/usr/local/go/bin/go version 2>/dev/null || echo 'Go not installed')\"
        echo \"Memory: \$(free -h | grep Mem | awk '{print \$2}')\"
        echo \"CPU: \$(nproc) cores\"
    "
}

# Установка Go на удалённом хосте (если требуется)
install_go() {
    local host="$1"
    log_info "Проверка установки Go на хосте $host..."
    
    local go_installed
    go_installed=$(ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        /usr/local/go/bin/go version 2>/dev/null && echo 'installed' || echo 'not_installed'
    ")
    
    if [ "$go_installed" == "not_installed" ]; then
        log_warn "Go не установлен на хосте $host. Установка..."
        ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
            cd /tmp
            wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
            rm -rf /usr/local/go
            tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
            rm go${GO_VERSION}.linux-amd64.tar.gz
            echo 'export PATH=/usr/local/go/bin:\$PATH' >> ~/.bashrc
        "
        log_success "Go установлен на хосте $host"
    else
        log_success "Go уже установлен на хосте $host"
    fi
}

# Копирование файлов на удалённый хост
copy_files() {
    local host="$1"
    log_info "Копирование файлов на хост $host..."
    
    # Создаём директорию
    ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "mkdir -p $REMOTE_DIR"
    
    # Копируем исходный код
    rsync -avz --exclude='.git' --exclude='bin' --exclude='dist' \
        -e "ssh -o StrictHostKeyChecking=no" \
        "$LOCAL_DIR/" "$REMOTE_USER@$host:$REMOTE_DIR/"
    
    log_success "Файлы скопированы на хост $host"
}

# Запуск тестов на удалённом хосте
run_tests() {
    local host="$1"
    local report_file="test_report_${host}_$(date +%Y%m%d_%H%M%S).txt"
    
    log_info "Запуск тестов на хосте $host..."
    
    ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        echo '=========================================='
        echo 'Network Monitor Test Report'
        echo '=========================================='
        echo \"Host: \$(hostname)\"
        echo \"Date: \$(date)\"
        echo \"Kernel: \$(uname -r)\"
        echo \"Go: \$(go version)\"
        echo '=========================================='
        echo ''
        
        # Загрузка зависимостей
        echo '[1/4] Загрузка зависимостей...'
        go mod download
        
        # Unit тесты
        echo ''
        echo '[2/4] Unit тесты...'
        echo '------------------------------------------'
"

    if [ "$RUN_UNIT_TESTS" = true ]; then
        ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        if go test -v -timeout=5m ./internal/packetloss/... 2>&1; then
            echo '✅ packetloss тесты пройдены'
        else
            echo '❌ packetloss тесты не пройдены'
        fi
        
        if go test -v -timeout=5m ./internal/bandwidth/... 2>&1; then
            echo '✅ bandwidth тесты пройдены'
        else
            echo '❌ bandwidth тесты не пройдены'
        fi
        
        if go test -v -timeout=5m ./internal/collector/... 2>&1; then
            echo '✅ collector тесты пройдены'
        else
            echo '❌ collector тесты не пройдены'
        fi
        
        if go test -v -timeout=5m ./internal/discovery/... 2>&1; then
            echo '✅ discovery тесты пройдены'
        else
            echo '❌ discovery тесты не пройдены'
        fi
        
        echo ''
        echo '------------------------------------------'
"
    fi

    ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        # Integration тесты
        echo ''
        echo '[3/4] Integration тесты (требуют root)...'
        echo '------------------------------------------'
"

    if [ "$RUN_INTEGRATION_TESTS" = true ]; then
        ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        if go test -v -tags=integration -timeout=10m ./internal/collector/... 2>&1; then
            echo '✅ collector integration тесты пройдены'
        else
            echo '❌ collector integration тесты не пройдены'
        fi
        
        if go test -v -tags=integration -timeout=10m ./tests/integration/... 2>&1; then
            echo '✅ integration тесты пройдены'
        else
            echo '❌ integration тесты не пройдены'
        fi
        
        echo ''
        echo '------------------------------------------'
"
    fi

    ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        # Покрытие
        echo ''
        echo '[4/4] Генерация отчёта о покрытии...'
        echo '------------------------------------------'
"

    if [ "$GENERATE_COVERAGE" = true ]; then
        ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        go test -coverprofile=coverage.out ./internal/... 2>&1 | tee coverage_summary.txt
        echo ''
        echo '=== Coverage Summary ==='
        go tool cover -func=coverage.out | grep -E '(packetloss|bandwidth|collector):' || true
        echo ''
        
        # HTML отчёт (если есть)
        if command -v go &> /dev/null; then
            go tool cover -html=coverage.out -o coverage.html 2>/dev/null && \
                echo 'HTML отчёт: $REMOTE_DIR/coverage.html' || true
        fi
"
    fi

    ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "
        export PATH=/usr/local/go/bin:\$PATH
        cd $REMOTE_DIR
        
        echo ''
        echo '=========================================='
        echo 'Test Complete'
        echo '=========================================='
        echo \"Finished at: \$(date)\"
    "
    
    # Копирование отчёта
    log_info "Копирование отчёта с хоста $host..."
    scp -o StrictHostKeyChecking=no "$REMOTE_USER@$host:$REMOTE_DIR/coverage.out" "./scripts/${report_file}.coverage.out" 2>/dev/null || true
    scp -o StrictHostKeyChecking=no "$REMOTE_USER@$host:$REMOTE_DIR/coverage_summary.txt" "./scripts/${report_file}.summary.txt" 2>/dev/null || true
    
    log_success "Тесты на хосте $host завершены"
}

# Очистка удалённых файлов
cleanup() {
    local host="$1"
    
    if [ "$CLEANUP" = true ]; then
        log_info "Очистка файлов на хосте $host..."
        ssh -o StrictHostKeyChecking=no "$REMOTE_USER@$host" "rm -rf $REMOTE_DIR"
        log_success "Очистка завершена"
    else
        log_info "Очистка пропущена (файлы остались в $REMOTE_DIR)"
    fi
}

# Основная функция
main() {
    echo '=========================================='
    echo 'Network Monitor Remote Test Runner'
    echo '=========================================='
    echo ''
    
    check_dependencies
    
    local hosts
    hosts=$(get_hosts "$@")
    
    log_info "Хосты для тестирования: $hosts"
    
    for host in $hosts; do
        echo ''
        echo '=========================================='
        log_info "Обработка хоста: $host"
        echo '=========================================='
        
        if ! check_host "$host"; then
            log_warn "Пропуск хоста $host"
            continue
        fi
        
        get_host_info "$host"
        install_go "$host"
        copy_files "$host"
        run_tests "$host"
        cleanup "$host"
    done
    
    echo ''
    echo '=========================================='
    log_success "Все тесты завершены!"
    echo '=========================================='
    echo ''
    echo 'Отчёты сохранены в ./scripts/'
    echo '  - test_report_*.summary.txt'
    echo '  - test_report_*.coverage.out'
}

# Запуск
main "$@"

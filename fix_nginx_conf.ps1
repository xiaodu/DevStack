# Fix nginx.conf - regenerate clean config preserving sites-enabled
$confPath = "E:\DevStack\nginx\1.26.0\conf\nginx.conf"
$wwwRoot = "E:/DevStack/www"
$port = 80

# Backup current config
$backupPath = "$confPath.backup.$(Get-Date -Format 'yyyyMMdd_HHmmss')"
Copy-Item $confPath $backupPath -Force
Write-Host "Backup created: $backupPath"

# Generate clean optimized config
$config = @"
worker_processes auto;
error_log logs/error.log;
pid logs/nginx.pid;

events {
    worker_connections 2048;
    multi_accept on;
    use select;
}

http {
    include       mime.types;
    default_type  application/octet-stream;
    charset utf-8;

    # Connection optimization
    sendfile        on;
    tcp_nopush      on;
    tcp_nodelay     on;
    keepalive_timeout  65;
    keepalive_requests 1000;
    reset_timedout_connection on;

    access_log logs/access.log;

    # Security
    server_tokens off;
    client_max_body_size 64m;
    client_body_buffer_size 128k;
    client_header_buffer_size 1k;
    large_client_header_buffers 4 16k;

    # gzip compression
    gzip on;
    gzip_min_length 1k;
    gzip_buffers 4 16k;
    gzip_comp_level 5;
    gzip_types text/plain text/css text/xml text/javascript application/javascript application/json application/xml application/rss+xml application/atom+xml image/svg+xml;
    gzip_vary on;
    gzip_proxied any;
    gzip_disable "msie6";

    # FastCGI buffer
    fastcgi_buffer_size 128k;
    fastcgi_buffers 8 256k;
    fastcgi_busy_buffers_size 256k;
    fastcgi_temp_file_write_size 256k;
    fastcgi_intercept_errors on;

    # FastCGI cache
    fastcgi_cache_path cache levels=1:2 keys_zone=phpcache:100m inactive=60m max_size=512m use_temp_path=off;
    fastcgi_cache_key "`$scheme`$request_method`$host`$request_uri";
    fastcgi_cache_use_stale error timeout invalid_header http_500;
    fastcgi_ignore_client_abort on;

    # Rate limiting
    limit_req_zone `$binary_remote_addr zone=php:10m rate=10r/s;
    limit_conn_zone `$binary_remote_addr zone=conn:10m;

    upstream php_backend {
        server 127.0.0.1:9000;
        server 127.0.0.1:9001 backup;
        server 127.0.0.1:9002 backup;
        server 127.0.0.1:9003 backup;
    }

    # Default server - localhost access to www directory
    server {
        listen       $port default_server;
        server_name  localhost _;

        # Security headers
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-XSS-Protection "1; mode=block" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;

        location / {
            root   $wwwRoot;
            index  index.php index.html index.htm;
            try_files `$uri `$uri/ =404;
        }

        location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot|webp)$ {
            root $wwwRoot;
            expires 7d;
            add_header Cache-Control "public, no-transform, immutable";
            access_log off;
            add_header X-Content-Type-Options "nosniff";
        }

        error_page   500 502 503 504  /50x.html;
        location = /50x.html {
            root   $wwwRoot;
        }

        location ~ \.php$ {
            root           $wwwRoot;
            fastcgi_pass   php_backend;
            fastcgi_index  index.php;
            fastcgi_param  SCRIPT_FILENAME  `$document_root`$fastcgi_script_name;
            include        fastcgi_params;
            fastcgi_read_timeout 300s;

            limit_req zone=php burst=20 nodelay;
            limit_conn conn 10;
        }

        location ~ /\. {
            deny all;
            access_log off;
            log_not_found off;
        }
    }

    include sites-enabled/*.conf;
}
"@

Set-Content -Path $confPath -Value $config -Encoding UTF8
Write-Host "nginx.conf regenerated successfully!"

# Verify syntax
$nginxExe = "E:\DevStack\nginx\1.26.0\nginx.exe"
& $nginxExe -t -c $confPath 2>&1

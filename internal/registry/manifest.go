package registry

func Builtin() *Manifest {
	return &Manifest{
		Packages: []PackageDef{
			{
				ID: "nginx", Name: "Nginx", Type: PkgNginx, Category: CatWebServer,
				DefaultVersion: "1.26.0", DefaultPort: 80,
				Versions: []VersionInfo{
					{Version: "1.22.1", URL: "https://nginx.org/download/nginx-1.22.1.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-1.22.1.zip", FileName: "nginx-1.22.1.zip"},
					{Version: "1.24.0", URL: "https://nginx.org/download/nginx-1.24.0.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-1.24.0.zip", FileName: "nginx-1.24.0.zip"},
					{Version: "1.26.0", URL: "https://nginx.org/download/nginx-1.26.0.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nginx/nginx-1.26.0.zip", FileName: "nginx-1.26.0.zip"},
				},
			},
			{
				ID: "mysql", Name: "MySQL", Type: PkgMySQL, Category: CatDatabase,
				DefaultVersion: "8.4.0", DefaultPort: 3306,
				Versions: []VersionInfo{
					{Version: "5.6.51", URL: "https://dev.mysql.com/get/Downloads/MySQL-5.6/mysql-5.6.51-winx64.zip", Mirror: "https://mirrors.aliyun.com/mysql/MySQL-5.6/mysql-5.6.51-winx64.zip", FileName: "mysql-5.6.51-winx64.zip"},
					{Version: "5.7.44", URL: "https://dev.mysql.com/get/Downloads/MySQL-5.7/mysql-5.7.44-winx64.zip", Mirror: "https://mirrors.aliyun.com/mysql/MySQL-5.7/mysql-5.7.44-winx64.zip", FileName: "mysql-5.7.44-winx64.zip"},
					{Version: "8.0.36", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.0/mysql-8.0.36-winx64.zip", Mirror: "https://mirrors.aliyun.com/mysql/MySQL-8.0/mysql-8.0.36-winx64.zip", FileName: "mysql-8.0.36-winx64.zip"},
					{Version: "8.4.0", URL: "https://dev.mysql.com/get/Downloads/MySQL-8.4/mysql-8.4.0-winx64.zip", Mirror: "https://mirrors.aliyun.com/mysql/MySQL-8.4/mysql-8.4.0-winx64.zip", FileName: "mysql-8.4.0-winx64.zip"},
				},
			},
			{
				ID: "redis", Name: "Redis", Type: PkgRedis, Category: CatService,
				DefaultVersion: "7.2.0", DefaultPort: 6379,
				Versions: []VersionInfo{
					{Version: "5.0.14", URL: "https://github.com/redis-windows/redis-windows/releases/download/5.0.14/Redis-5.0.14-Windows-x64.zip", FileName: "Redis-5.0.14-Windows-x64.zip"},
					{Version: "6.0.20", URL: "https://github.com/redis-windows/redis-windows/releases/download/6.0.20/Redis-6.0.20-Windows-x64.zip", FileName: "Redis-6.0.20-Windows-x64.zip"},
					{Version: "7.2.0", URL: "https://github.com/redis-windows/redis-windows/releases/download/7.2.0/Redis-7.2.0-Windows-x64.zip", FileName: "Redis-7.2.0-Windows-x64.zip"},
					{Version: "7.4.0", URL: "https://github.com/redis-windows/redis-windows/releases/download/7.4.0/Redis-7.4.0-Windows-x64.zip", FileName: "Redis-7.4.0-Windows-x64.zip"},
				},
			},
			{
				ID: "php", Name: "PHP", Type: PkgPHP, Category: CatRuntime,
				DefaultVersion: "8.3.32", DefaultPort: 9000,
				Versions: []VersionInfo{
					{Version: "5.5.38", URL: "https://downloads.php.net/~windows/releases/archives/php-5.5.38-nts-Win32-VC11-x64.zip", FileName: "php-5.5.38.zip"},
					{Version: "5.6.40", URL: "https://downloads.php.net/~windows/releases/archives/php-5.6.40-nts-Win32-VC11-x64.zip", FileName: "php-5.6.40.zip"},
					{Version: "7.0.33", URL: "https://downloads.php.net/~windows/releases/archives/php-7.0.33-nts-Win32-VC14-x64.zip", FileName: "php-7.0.33.zip"},
					{Version: "7.1.33", URL: "https://downloads.php.net/~windows/releases/archives/php-7.1.33-nts-Win32-VC14-x64.zip", FileName: "php-7.1.33.zip"},
					{Version: "7.2.34", URL: "https://downloads.php.net/~windows/releases/archives/php-7.2.34-nts-Win32-VC15-x64.zip", FileName: "php-7.2.34.zip"},
					{Version: "7.3.33", URL: "https://downloads.php.net/~windows/releases/archives/php-7.3.33-nts-Win32-VC15-x64.zip", FileName: "php-7.3.33.zip"},
					{Version: "7.4.33", URL: "https://downloads.php.net/~windows/releases/php-7.4.33-nts-Win32-vc15-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-7.4.33-nts-Win32-vc15-x64.zip", FileName: "php-7.4.33.zip"},
					{Version: "8.0.30", URL: "https://downloads.php.net/~windows/releases/php-8.0.30-nts-Win32-vs16-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-8.0.30-nts-Win32-vs16-x64.zip", FileName: "php-8.0.30.zip"},
					{Version: "8.1.34", URL: "https://downloads.php.net/~windows/releases/php-8.1.34-nts-Win32-vs16-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-8.1.34-nts-Win32-vs16-x64.zip", FileName: "php-8.1.34.zip"},
					{Version: "8.2.32", URL: "https://downloads.php.net/~windows/releases/php-8.2.32-nts-Win32-vs16-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-8.2.32-nts-Win32-vs16-x64.zip", FileName: "php-8.2.32.zip"},
					{Version: "8.3.32", URL: "https://downloads.php.net/~windows/releases/php-8.3.32-nts-Win32-vs16-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-8.3.32-nts-Win32-vs16-x64.zip", FileName: "php-8.3.32.zip"},
					{Version: "8.4.23", URL: "https://downloads.php.net/~windows/releases/php-8.4.23-nts-Win32-vs17-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-8.4.23-nts-Win32-vs17-x64.zip", FileName: "php-8.4.23.zip"},
					{Version: "8.5.8", URL: "https://downloads.php.net/~windows/releases/php-8.5.8-nts-Win32-vs17-x64.zip", Mirror: "https://mirrors.aliyun.com/php/php-8.5.8-nts-Win32-vs17-x64.zip", FileName: "php-8.5.8.zip"},
				},
			},
			{
				ID: "node", Name: "Node.js", Type: PkgNode, Category: CatRuntime,
				DefaultVersion: "20.18.0", DefaultPort: 3000,
				Versions: []VersionInfo{
					{Version: "16.20.2", URL: "https://nodejs.org/dist/v16.20.2/node-v16.20.2-win-x64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v16.20.2/node-v16.20.2-win-x64.zip", FileName: "node-v16.20.2-win-x64.zip"},
					{Version: "18.20.4", URL: "https://nodejs.org/dist/v18.20.4/node-v18.20.4-win-x64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v18.20.4/node-v18.20.4-win-x64.zip", FileName: "node-v18.20.4-win-x64.zip"},
					{Version: "20.18.0", URL: "https://nodejs.org/dist/v20.18.0/node-v20.18.0-win-x64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v20.18.0/node-v20.18.0-win-x64.zip", FileName: "node-v20.18.0-win-x64.zip"},
					{Version: "22.8.0", URL: "https://nodejs.org/dist/v22.8.0/node-v22.8.0-win-x64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v22.8.0/node-v22.8.0-win-x64.zip", FileName: "node-v22.8.0-win-x64.zip"},
				},
			},
			{
				ID: "python", Name: "Python", Type: PkgPython, Category: CatRuntime,
				DefaultVersion: "3.12.8", DefaultPort: 3001,
				Versions: []VersionInfo{
					{Version: "3.9.13", URL: "https://www.python.org/ftp/python/3.9.13/python-3.9.13-embed-amd64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/python/3.9.13/python-3.9.13-embed-amd64.zip", FileName: "python-3.9.13-embed-amd64.zip"},
					{Version: "3.10.11", URL: "https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/python/3.10.11/python-3.10.11-embed-amd64.zip", FileName: "python-3.10.11-embed-amd64.zip"},
					{Version: "3.11.11", URL: "https://www.python.org/ftp/python/3.11.11/python-3.11.11-embed-amd64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/python/3.11.11/python-3.11.11-embed-amd64.zip", FileName: "python-3.11.11-embed-amd64.zip"},
					{Version: "3.12.8", URL: "https://www.python.org/ftp/python/3.12.8/python-3.12.8-embed-amd64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/python/3.12.8/python-3.12.8-embed-amd64.zip", FileName: "python-3.12.8-embed-amd64.zip"},
					{Version: "3.13.2", URL: "https://www.python.org/ftp/python/3.13.2/python-3.13.2-embed-amd64.zip", Mirror: "https://mirrors.tuna.tsinghua.edu.cn/python/3.13.2/python-3.13.2-embed-amd64.zip", FileName: "python-3.13.2-embed-amd64.zip"},
				},
			},
			{
				ID: "composer", Name: "Composer", Type: PkgComposer, Category: CatRuntime,
				DefaultVersion: "2.7.7", DefaultPort: 0,
				Versions: []VersionInfo{
					{Version: "2.5.8", URL: "https://getcomposer.org/download/2.5.8/composer.phar", FileName: "composer.phar"},
					{Version: "2.6.6", URL: "https://getcomposer.org/download/2.6.6/composer.phar", FileName: "composer.phar"},
					{Version: "2.7.7", URL: "https://getcomposer.org/download/2.7.7/composer.phar", FileName: "composer.phar"},
				},
			},
			{
				ID: "apache", Name: "Apache", Type: PkgApache, Category: CatWebServer,
				DefaultVersion: "2.4.62", DefaultPort: 8080,
				Versions: []VersionInfo{
					{Version: "2.4.58", URL: "https://www.apachelounge.com/download/VS17/binaries/httpd-2.4.58-win64-VS17.zip", Mirror: "https://mirrors.aliyun.com/apache/httpd-2.4.58-win64.zip", FileName: "httpd-2.4.58.zip"},
					{Version: "2.4.62", URL: "https://www.apachelounge.com/download/VS17/binaries/httpd-2.4.62-win64-VS17.zip", Mirror: "https://mirrors.aliyun.com/apache/httpd-2.4.62-win64.zip", FileName: "httpd-2.4.62.zip"},
				},
			},
			{
				ID: "postgresql", Name: "PostgreSQL", Type: PkgPostgreSQL, Category: CatDatabase,
				DefaultVersion: "16.3", DefaultPort: 5432,
				Versions: []VersionInfo{
					{Version: "15.7", URL: "https://get.enterprisedb.com/postgresql/postgresql-15.7-1-windows-x64.zip", FileName: "postgresql-15.7.zip"},
					{Version: "16.3", URL: "https://get.enterprisedb.com/postgresql/postgresql-16.3-1-windows-x64.zip", FileName: "postgresql-16.3.zip"},
				},
			},
			{
				ID: "mongodb", Name: "MongoDB", Type: PkgMongoDB, Category: CatDatabase,
				DefaultVersion: "7.0.11", DefaultPort: 27017,
				Versions: []VersionInfo{
					{Version: "6.0.16", URL: "https://fastdl.mongodb.org/windows/mongodb-windows-x86_64-6.0.16.zip", FileName: "mongodb-6.0.16.zip"},
					{Version: "7.0.11", URL: "https://fastdl.mongodb.org/windows/mongodb-windows-x86_64-7.0.11.zip", FileName: "mongodb-7.0.11.zip"},
				},
			},
			{
				ID: "memcached", Name: "Memcached", Type: PkgMemcached, Category: CatService,
				DefaultVersion: "1.6.27", DefaultPort: 11211,
				Versions: []VersionInfo{
					{Version: "1.6.24", URL: "https://github.com/nickfox-tatered/memcached-windows/releases/download/1.6.24/memcached-1.6.24.zip", FileName: "memcached-1.6.24.zip"},
					{Version: "1.6.27", URL: "https://github.com/nickfox-tatered/memcached-windows/releases/download/1.6.27/memcached-1.6.27.zip", FileName: "memcached-1.6.27.zip"},
				},
			},
		},
	}
}

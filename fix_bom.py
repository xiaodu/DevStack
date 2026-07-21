import sys

conf_path = r'E:\DevStack\nginx\1.26.0\conf\nginx.conf'

with open(conf_path, 'rb') as f:
    content = f.read()

# Remove BOM if present
if content.startswith(b'\xef\xbb\xbf'):
    content = content[3:]
    with open(conf_path, 'wb') as f:
        f.write(content)
    print('BOM removed successfully')
else:
    print('No BOM found')

# Verify first line
with open(conf_path, 'r', encoding='utf-8') as f:
    first_line = f.readline()
    print(f'First line: {repr(first_line[:50])}')

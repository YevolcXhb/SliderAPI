import re

# Fix 1: cmd/migcheck/main.go
with open('cmd/migcheck/main.go', 'r', encoding='utf-8') as f:
    content = f.read()
content = content.replace('defer db.Close()', 'defer func() { _ = db.Close() }()')
content = content.replace('defer rows.Close()', 'defer func() { _ = rows.Close() }()')
with open('cmd/migcheck/main.go', 'w', encoding='utf-8') as f:
    f.write(content)
print('Fixed cmd/migcheck/main.go')

# Fix 2: internal/pkg/dbdialect/array.go
with open('internal/pkg/dbdialect/array.go', 'r', encoding='utf-8') as f:
    content = f.read()
content = content.replace('sb.WriteString("")', '_ = sb.WriteString("")')
content = content.replace('sb.WriteString(fmt.Sprint(v))', '_ = sb.WriteString(fmt.Sprint(v))')
with open('internal/pkg/dbdialect/array.go', 'w', encoding='utf-8') as f:
    f.write(content)
print('Fixed internal/pkg/dbdialect/array.go')

# Fix 3: internal/pkg/dbdialect/mysql_compat.go
with open('internal/pkg/dbdialect/mysql_compat.go', 'r', encoding='utf-8') as f:
    lines = f.readlines()
for i, line in enumerate(lines):
    if 'sb.WriteString' in line and '_ =' not in line:
        lines[i] = line.replace('sb.WriteString', '_ = sb.WriteString')
    if 'sb.WriteByte' in line and '_ =' not in line:
        lines[i] = line.replace('sb.WriteByte', '_ = sb.WriteByte')
with open('internal/pkg/dbdialect/mysql_compat.go', 'w', encoding='utf-8') as f:
    f.writelines(lines)
print('Fixed internal/pkg/dbdialect/mysql_compat.go')

# Fix 4: internal/repository/channel_monitor_v2_aggregation.go
with open('internal/repository/channel_monitor_v2_aggregation.go', 'r', encoding='utf-8') as f:
    content = f.read()
content = content.replace('defer rows.Close()', 'defer func() { _ = rows.Close() }()')
with open('internal/repository/channel_monitor_v2_aggregation.go', 'w', encoding='utf-8') as f:
    f.write(content)
print('Fixed internal/repository/channel_monitor_v2_aggregation.go')

# Fix 5: internal/repository/migrations_runner.go
with open('internal/repository/migrations_runner.go', 'r', encoding='utf-8') as f:
    lines = f.readlines()
for i, line in enumerate(lines):
    if 'sb.WriteRune' in line and '_ =' not in line:
        lines[i] = line.replace('sb.WriteRune', '_ = sb.WriteRune')
with open('internal/repository/migrations_runner.go', 'w', encoding='utf-8') as f:
    f.writelines(lines)
print('Fixed internal/repository/migrations_runner.go')

print('All errcheck fixes applied!')

BEGIN {
    print "TAP version 14"
    n = 0
    current = ""
    split("", closed)
}

function classify(line,    trimmed) {
    trimmed = line
    sub(/^[[:space:]]+/, "", trimmed)
    if (trimmed ~ /^Enumerating objects:/ || trimmed ~ /^Counting objects:/ || trimmed ~ /^Delta compression/ || trimmed ~ /^Compressing objects:/ || trimmed ~ /^Writing objects:/) {
        return "pack"
    }
    if (trimmed ~ /^Total / || trimmed ~ /^To /) {
        return "transfer"
    }
    if ((line ~ /^ / && trimmed ~ /->/) || trimmed ~ /\[new branch\]/ || trimmed ~ /\[new tag\]/) {
        return "summary"
    }
    return ""
}

function emit_phase() {
    if (current != "") {
        n++
        printf "ok %d - %s\n", n, current
        closed[current] = 1
    }
}

{
    phase = classify($0)

    printf "# %s\n", $0

    if (phase != "" && phase != current && !(phase in closed)) {
        emit_phase()
        current = phase
    }
}

END {
    emit_phase()
    printf "1..%d\n", n
}

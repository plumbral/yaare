$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$iconRoot = Join-Path $projectRoot 'frontend\src\shared\icons\os'
$fontCommit = 'd3bf5d299e54595db1b19681a0cc57ab10454857'
$fontIcons = [ordered]@{
    redhat='redhat'; centos='centos'; rockylinux='rocky-linux'; almalinux='almalinux'
    opensuse='opensuse'; gentoo='gentoo'; nixos='nixos'; freebsd='freebsd'
    openbsd='openbsd'; slackware='slackware'; devuan='devuan'; raspberrypi='raspberry-pi'
    manjaro='manjaro'; linuxmint='linuxmint'; kalilinux='kali-linux'
}
$catalog = Get-Content -LiteralPath (Join-Path $iconRoot 'paths.json') -Raw | ConvertFrom-Json
$glyphs = [ordered]@{}
foreach ($name in @('linux','ubuntu','debian','alpinelinux','archlinux','fedora')) {
    $old = $catalog.$name
    if ($old -is [string]) { $glyphs[$name] = @{viewBox='0 0 24 24';paths=@(@{d=$old})} }
    else { $glyphs[$name] = $old }
}
foreach ($entry in $fontIcons.GetEnumerator()) {
    $uri = "https://raw.githubusercontent.com/Lukas-W/font-logos/$fontCommit/vectors/$($entry.Value).svg"
    $source = (Invoke-WebRequest -UseBasicParsing -Uri $uri).Content
    if ($entry.Key -eq 'openbsd') {
        $inputFile = [IO.Path]::GetTempFileName()
        $outputFile = [IO.Path]::GetTempFileName()
        try {
            [IO.File]::WriteAllText($inputFile, $source)
            & npx.cmd --yes svgo@4.0.0 --quiet -p 1 -i $inputFile -o $outputFile
            if ($LASTEXITCODE -ne 0) { throw 'OpenBSD SVG optimization failed.' }
            $source = [IO.File]::ReadAllText($outputFile)
        } finally {
            Remove-Item -LiteralPath $inputFile,$outputFile
        }
    }
    [xml]$document = $source
    $svg = $document.DocumentElement
    $viewBox = $svg.GetAttribute('viewBox')
    if (-not $viewBox) { $viewBox = '0 0 ' + $svg.GetAttribute('width').Replace('px','') + ' ' + $svg.GetAttribute('height').Replace('px','') }
    $paths = @()
    foreach ($node in $document.SelectNodes('//*[local-name()="path" or local-name()="rect"]')) {
        $parts = @(); $ancestor = $node
        while ($ancestor -and $ancestor -ne $svg) {
            if ($ancestor.HasAttribute('transform')) { $parts = @($ancestor.GetAttribute('transform')) + $parts }
            $ancestor = $ancestor.ParentNode
        }
        $geometry = $node.GetAttribute('d')
        if ($node.LocalName -eq 'rect') {
            if ($node.HasAttribute('rx') -or $node.HasAttribute('ry')) { throw 'Rounded rectangle requires path conversion.' }
            $x = if ($node.HasAttribute('x')) { $node.GetAttribute('x') } else { '0' }
            $y = if ($node.HasAttribute('y')) { $node.GetAttribute('y') } else { '0' }
            $w = $node.GetAttribute('width'); $h = $node.GetAttribute('height')
            $geometry = "M $x $y h $w v $h h -$w Z"
        }
        $path = [ordered]@{d=$geometry}
        if ($parts.Count) { $path['transform']=$parts -join ' ' }
        if ($node.GetAttribute('fill-rule') -eq 'evenodd' -or $node.GetAttribute('style') -match 'fill-rule:\s*evenodd') { $path['fillRule']='evenodd' }
        $paths += $path
    }
    if (-not $paths.Count -or $document.SelectNodes('//*[local-name()="circle" or local-name()="ellipse" or local-name()="polygon" or local-name()="use"]').Count) { throw "Unsupported SVG geometry: $($entry.Key)" }
    $glyphs[$entry.Key] = @{viewBox=$viewBox;paths=$paths}
}
$encoding = New-Object Text.UTF8Encoding($false)
[IO.File]::WriteAllText((Join-Path $iconRoot 'paths.json'),($glyphs | ConvertTo-Json -Depth 12),$encoding)
foreach ($entry in $glyphs.GetEnumerator()) {
    $glyph = $entry.Value
    $body = foreach ($path in $glyph.paths) {
        $transform = if ($path.transform) { ' transform="' + [Security.SecurityElement]::Escape($path.transform) + '"' } else { '' }
        $rule = if ($path.fillRule) { ' fill-rule="evenodd"' } else { '' }
        '<path fill="currentColor" d="' + [Security.SecurityElement]::Escape($path.d) + '"' + $transform + $rule + '/>'
    }
    $asset = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="' + $glyph.viewBox + '">' + ($body -join '') + '</svg>'
    [IO.File]::WriteAllText((Join-Path $iconRoot ($entry.Key + '.svg')),$asset,$encoding)
}
$license = (Invoke-WebRequest -UseBasicParsing -Uri "https://raw.githubusercontent.com/Lukas-W/font-logos/$fontCommit/LICENSE").Content
[IO.File]::WriteAllText((Join-Path $iconRoot 'Font-Logos-LICENSE.txt'),$license,$encoding)
[IO.File]::WriteAllText((Join-Path $projectRoot 'resources\licenses\font-logos-LICENSE'),$license,$encoding)
Copy-Item -LiteralPath (Join-Path $iconRoot 'LICENSE.md') -Destination (Join-Path $projectRoot 'resources\licenses\simple-icons-LICENSE') -Force
[IO.File]::WriteAllText((Join-Path $iconRoot 'SOURCE.txt'),"Original six: Simple Icons, commit cd796c98565fc466923fc1128b962e133fa3c612`nAdditional glyphs: Lukas-W/font-logos, commit $fontCommit`nGenerated local monochrome SVG assets and React geometry; no runtime network dependency.`n",$encoding)
Write-Output ('Generated {0} local OS icons.' -f $glyphs.Count)

#!/usr/bin/env python3
"""Post-run input verification only; does not claim pre/mid-confirmation checks."""
from pathlib import Path
from datetime import datetime, timezone
import hashlib
import json

root = Path(__file__).resolve().parent
confirmation = root / 'round12-single-confirmation'
first = root / 'round12-review'
complete = json.loads((confirmation / 'complete.json').read_text())
assert complete['complete'] and complete['cases'] == 5 and complete['pairs_per_case'] == 6
provenance = json.loads((first / 'provenance.json').read_text())
metadata = json.loads((confirmation / 'provenance.json').read_text())
destination = confirmation / 'post-run-input-verification.json'
assert not destination.exists(), 'Existing verification must be retained'
started = datetime.now(timezone.utc).isoformat()
verified, rows = {}, []
for case in metadata['cases']:
    path = Path(case['file']).resolve()
    expected = provenance['input_sha256'][str(path)]
    if str(path) not in verified:
        digest = hashlib.sha256()
        with path.open('rb') as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(block)
        verified[str(path)] = {'sha256': digest.hexdigest(), 'bytes': path.stat().st_size}
    record = verified[str(path)]
    rows.append({'case': case, 'resolved_path': str(path),
                 'first_pass_validated_sha256': expected, 'post_confirmation_sha256': record['sha256'],
                 'bytes': record['bytes'], 'matches': expected == record['sha256']})
result = {'scope': 'Post-run verification against the validated first-pass input hashes; no initial or mid-confirmation fixture check was performed by the confirmation driver.',
          'started_utc': started, 'finished_utc': datetime.now(timezone.utc).isoformat(),
          'confirmation_complete': complete, 'case_count': len(rows), 'distinct_input_files': len(verified),
          'all_match': all(row['matches'] for row in rows), 'cases': rows,
          'reads_only': True, 'first_pass_provenance': str(first / 'provenance.json')}
destination.write_text(json.dumps(result, indent=2) + '\n')
assert result['all_match'], f'Input mismatch: see {destination}'
print(destination)

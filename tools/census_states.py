#!/usr/bin/env python3
"""Regenerate embedded masks from Census cb_2024_us_state_500k.zip (stdlib only).
Usage: python3 tools/census_states.py /path/to/cb_2024_us_state_500k.zip
Source: https://www2.census.gov/geo/tiger/GENZ2024/shp/cb_2024_us_state_500k.zip
"""
import gzip, json, pathlib, struct, sys, zipfile
z=zipfile.ZipFile(sys.argv[1]);dbf=z.read(next(x for x in z.namelist() if x.endswith('.dbf')))
n=struct.unpack_from('<I',dbf,4)[0];h,length=struct.unpack_from('<HH',dbf,8);fields=[];p=32
while dbf[p]!=13:
 fields.append((dbf[p:p+11].split(b'\0')[0].decode(),dbf[p+16]));p+=32
rows=[]
for i in range(n):
 rec=dbf[h+i*length:h+(i+1)*length];p=1;r={}
 for name,size in fields:r[name]=rec[p:p+size].decode().strip();p+=size
 rows.append(r)
shp=z.read(next(x for x in z.namelist() if x.endswith('.shp')));p=100;catalog=[];shapes={}
for row in rows:
 size=struct.unpack_from('>I',shp,p+4)[0]*2;p+=8;record=shp[p:p+size];p+=size
 if row['STUSPS'] in ['AK','HI','PR','GU','VI','MP','AS']:continue
 west,south,east,north=struct.unpack_from('<dddd',record,4)
 catalog.append(dict(code=row['STUSPS'],name=row['NAME'],bounds=dict(south=south,north=north,west=west,east=east)))
 parts,points=struct.unpack_from('<ii',record,36);starts=list(struct.unpack_from('<'+'i'*parts,record,44))+[points];offset=44+parts*4
 coords=[list(struct.unpack_from('<dd',record,offset+i*16)) for i in range(points)]
 shapes[row['STUSPS']]=[[[round(x,6),round(y,6)] for x,y in coords[starts[k]:starts[k+1]]] for k in range(parts)]
root=pathlib.Path(__file__).resolve().parent.parent/'internal/geo'
(root/'states.json').write_text(json.dumps(sorted(catalog,key=lambda s:s['name']),indent=2)+'\n')
(root/'state-masks.json.gz').write_bytes(gzip.compress(json.dumps(shapes,separators=(',',':'),sort_keys=True).encode(),mtime=0))
print(len(shapes),'state masks;',sum(len(r) for rings in shapes.values() for r in rings),'vertices')

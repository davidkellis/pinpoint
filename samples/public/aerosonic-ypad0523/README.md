# AeroSonicDB (YPAD-0523):  Labelled audio dataset for acoustic detection and classification of aircraft

Version 1.1.2 *(November 2023)*

### Created by
Blake Downward

### Publication
When using this data in an academic work, please reference the dataset DOI and version. Please also reference the following paper which describes the methodology for collecting the dataset and presents baseline model results.

Downward, B., & Nordby, J. (2023). The AeroSonicDB (YPAD-0523) Dataset for Acoustic Detection and Classification of Aircraft. [ArXiv, abs/2311.06368](https://arxiv.org/abs/2311.06368).

### Description
AeroSonicDB:YPAD-0523 is a specialised dataset of ADS-B labelled audio clips for research in the fields of environmental noise attribution and machine listening, particularly acoustic detection and classification of low-flying aircraft. Audio files in this dataset were recorded at locations in close proximity to a flight path approaching or departing Adelaide International Airport’s (ICAO code: YPAD) primary runway, 05/23. Recordings are initially labelled from radio (ADS-B) messages received from the aircraft overhead, then human verified and annotated with the first and final moments which the target aircraft is audible.

A total of 1,895 audio clips are distributed across two top-level classes, “Aircraft” (8.87 hours) and “Silence” (3.52 hours). The aircraft class is then further broken-down into four subclasses, which broadly describe the structure of the aircraft and propulsion mechanism. A variety of additional "airframe" features are provided to give researchers finer control of the dataset, and the opportunity to develop ontologies specific to their own use case.

For convenience, the dataset has been split into training (10.04 hours) and testing (2.35 hours) subsets, with the training set further split into 5 distinct folds for cross-validation. These splits are performed to prevent data-leakage between folds and the test set, ensuring samples collected in the same recording session (distinct in time, location and microphone) are assigned to the same fold.

Researchers may find applications for this dataset in a number of fields; particularly aircraft noise isolation and noise monitoring in an urban environment, development of passive acoustic systems to assist radar technology, and understanding the sources of aircraft noise to help manufacturers design less-noisy aircraft.


### Audio data
ADS-B (Automatic Dependent Surveillance–Broadcast) messages transmitted directly from aircraft are used to automatically trigger, capture and label audio samples. A 60-second recording is triggered when an aircraft transmits a message indicating it is within a specified distance of the recording device (see "Location data" below for specifics). The resulting audio file is labelled with the unique ICAO identifier code for the aircraft, as well as its last reported altitude, date, time, location and microphone. The recording is then human verified and annotated with timestamps for the first and last moments the aircraft is audible. In total, AeroSonicDB contains 625 recordings of low-altitude aircraft - varying in length from 18 to 60 seconds, for a total of 8.87 hours of aircraft audio.

A collection of urban background noise without aircraft (silence) is included with the dataset as a means of distinguishing location specific environmental noises from aircraft noises. 10-second background noise, or “silence” recordings are triggered only when there are no aircraft broadcasting they are within a specified distance of the recording device (see "Location data" below). These "silence" recordings are also human verified to ensure no aircraft noise is present. The dataset contains 1,270 clips of silence/urban background noise.


## Location data
Recordings were collected from three (3) locations. GPS coordinates for each location are provided in the "locations.json" file. In order to protect privacy, coordinates have been provided for a road or public space nearby the recording device instead of its exact location.

**Location ID: 0**

Situated in a suburban environment approximately 15.5km north-east of the start/end of the primary runway. For Adelaide, typical south-westerly winds bring most arriving aircraft past this location on approach. Winds from the north or east will cause aircraft to take-off to the north-east, however not all departing aircraft will maintain a course to trigger a recording at this location. The "trigger distance" for this location is set for 3km to ensure small/slower aircraft and large/faster aircraft are captured within a sixty-second recording.

"Silence" or ambient background noises at this location include; cars, motorbikes, light-trucks, garbage trucks, power-tools, lawn mowers, construction sounds, sirens, people talking, dogs barking and a wide range of Australian native birds (New Holland Honeyeaters, Wattlebirds, Australian Magpies, Australian Ravens, Spotted Doves, Rainbow Lorikeets and others).

**Location ID: 1**

Situated approximately 500m south-east of the south-eastern end of the runway, this location is nearby recreational areas (golf course, skate park and park-lands) with a busy road/highway in-between the location and runway. This location features heavy winds and road traffic, as well as people talking, walking and riding, and also birds such as the Australian Magpie and Noisy Miner. The trigger distance for this location is set to 1km. Due to their low altitude, aircraft are much louder but audible for a shorter time when compared to "Location 0".

**Location ID: 2**

As an alternative to "Location 1", this location is situated approximately 950m south-east of the end of the runway. This location has a wastewater facility to the north, a residential area to the south and a popular beach to the west. This location offers greater wind protection and further distance from airport and highway noises. Ambient background sounds feature close proximity cars and motorbikes, cyclists, people walking, nail guns and other construction sounds, as well as the local birds mentioned above.


### Aircraft metadata
Supplementary "airframe" metadata for all aircraft has been gathered to help broaden the research possibilities from this dataset. Airframe information was collected and cross-referenced from a number of open-source databases. The author has no reason to believe any significant errors exist in the "aircraft_meta" files, however future versions of this dataset plan to obtain aircraft information directly from ICAO (International Civil Aviation Organisation) to ensure a single, verifiable source of information. 

### Class/subclass ontology (minutes of recordings)

**Classes**

0. no aircraft (211)
1. aircraft (532)

**Subclasses**

0. no aircraft (211)
1. piston-propeller aeroplane (30)
2. turbine-propeller aeroplane (90)
3. turbine-fan aeroplane (409)
4. rotorcraft (4)
	
*The subclasses are a combination of the "airframe" and "engtype" features. Piston and Turboshaft helicopters (rotorcraft) have been combined into a single subclass due to the small number of samples collected.*
	
### Data splits
Audio recordings have been split into training (81%) and test (19%) sets. The training set has further been split into 5 folds, giving researchers a common split to perform 5-fold cross-validation to ensure reproducibility and comparable results. Data leakage into the test set has been avoided by ensuring recordings are disjointed from the training set by time and location - meaning samples in the test set for a particular location were recorded after any samples included in the training set for that particular location.

### Labelled data
The entire dataset (training and test) is referenced and labelled in the "sample_meta.csv" file. Each row contains a reference to a unique recording, its meta information, annotations and airframe features.

Alternatively, these labels can be derived directly from the filename of the sample (see below). The "aircraft_meta.csv" and "aircraft_meta.json" files can be used to reference aircraft specific features - such as; manufacturer, engine type, ICAO type designator etc. (see "Columns/Labels" below for all features).

### File naming convention
Audio samples are in *WAV* format, with some metadata stored in the filename.

**Basic Convention**

“*Aircraft ID* + *Date* + *Time* + *Location ID* + *Microphone ID*”

“XXXXXX_YYYY-MM-DD_hh-mm-ss_X_X”

**Sample with aircraft**

{hex_id} _ {date} _ {time} _ {location_id} _ {microphone_id} . {file_ext}

* 7C7CD0_2023-05-09_12-42-55_2_1.wav

**Sample without aircraft**

“Silence” files are denoted with six (6) leading zeros rather than an aircraft hex code. All relevant metadata for “silence” samples are contained in the audio filename, and again in the accompanying “sample_meta.csv”

000000 _ {date} _ {time} _ {location_id} _ {microphone_id} . {file_ext}

* 000000_2023-05-09_12-30-55_2_1.wav


### Columns/Labels 
(found in sample_meta.csv, aircraft_meta.csv/json files)

**train-test**: Train-test split (*train*, *test*)

**fold**: Digit from 1 to 5 splitting the training subset 5 ways (else *test*)

**filename**: The filename of the audio recording

**date**: Date of the recording

**time**: Time of the recording

**location**: ID for the location of the recording

**mic**: ID of the microphone used

**class**: Top-level label for the recording (eg. 0 = No aircraft, 1 = Aircraft audible)

**subclass**: Subclass label for the recording (eg. 0 = No aircraft, 3 = Turbine-fan aeroplane)

**altitude**: Approximate altitude of the aircraft (in feet) at the start of the recording

**hex_id**: Unique ICAO 24-bit address for the aircraft recorded

**session**: Unique recording session by time, location and microphone.

**offset**: Time stamp marking the start of the audio event.

**duration**: Length of the audio event in seconds.

**file_length**: Total length of the audio file in seconds.

**reg**: Registration number of the aircraft

**airframe**: Describes the mechanical structure of the aircraft (eg. Power Driven Aeroplane, Rotorcraft)

**engtype**: Type of engine (eg. Piston, Turboprop, Turbofan, Turboshaft)

**engnum**: Number of engines

**shortdesc**: 3 character alpha-numeric code describing the airframe and engine configuration (eg. L1P, L4J, H2T)

**typedesig**: ICAO type designator for make and model of aircraft (eg. PC12, C185, B738)

**manu**: Aircraft manufacturer (eg. Boeing, Pilatus, Airbus)

**model**: Aircraft model (eg. 737-800, A320-232, DHC-8-315)

**engmanu**: Engine manufacturer (eg. Pratt & Whitney, CFM International, Rolls Royce)

**engmodel**: Engine model (eg. TRENT XWB, CFM56-7B24E, PT6E-67XP)

**engfamily**: Family of the engine model (eg. TRENT, CFM56, PT6)

**fueltype**: Fuel type used in the engine (eg. Gasoline, Kerosene)

**propmanu**: Propeller manufacturer (eg. Hartzell Propellers, Hamilton Standard, "Aircraft Not Fitted With Propeller")

**propmodel**: Propeller model (eg. HC-E5A-3A\/NC10245B, 14SF-15, "Not Applicable")

**mtow**: Maximum take off weight (MTOW) in kilograms

 

### Environmental evaluation audio
As a means for evaluating model performance on real-world data, a supplementary set of real-time environmental recordings have been included with AeroSonicDB(YPAD-0523). This additional dataset contains six, one-hour long recordings of continuous urban noise, and is accompanied by a CSV file (environment_class_mappings.csv) annotated with relevant class labels per 5-second interval. Due to the variable length of an aircraft audio event and the lack of distinct onset and outset moments, audio segments which transition between aircraft and silence periods are tagged with an "ignore" class. This is done to provide a clear boundary between silence and aircraft events, helping to avoid false misclassification at event boundaries and ensure meaningful evaluation results.


## Conditions of use
Dataset created by Blake Downward.

The AeroSonic YPAD-0523 dataset is offered free of charge for non-commercial use under the Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0) license.

[https://creativecommons.org/licenses/by-nc/4.0/](https://creativecommons.org/licenses/by-nc/4.0/)

## Acknowledgements
Special thanks to Jon Nordby of Soundsensing AS - his contributions were pivotal in maximising the potential of this dataset for open-source release.

## Feedback
Please send suggestions, feedback and comments to:
Blake Downward: aerosonicdb@gmail.com


## Change log
* 1.1.2: Added "environment_mappings_raw.csv". No change to audio from Version 1.1
* 1.1.1: Minor change to "sample_meta.csv" - replaced "6" with "test" in the "fold" column
* 1.1: Replaced truncated aircraft samples with the original full-length files and annotated the beginning and end of each audio event. Added 'ignore' statements to aircraft event boundaries in the environmental class mappings file.
* 1.0: Environmental audio and mappings added
* 0.3: locations.json file added, README updated
* 0.2: location information added to README


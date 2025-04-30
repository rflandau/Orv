import sys
import time
import random
import requests
from http import HTTPStatus

'''
	// spawn the services we want to offer
	// TODO spin up a dummy server that just serves arbitrary files or just answers /ping

	// send a HELLO to a VK
	// TODO

	// send a JOIN request to that same VK
	// TODO

	// send a REGISTER request to that same VK to register our dummy services
	// TODO (this should probably be one REGISTER per service)

	// set up a heartbeat service to ping our vk every so often, per service (but less than a service's staleness)
'''

# Class to keep track of all global data for every user
class Options:
	CONNECTED = 1
	NOT_CONNECTED = 0
	MY_ID = random.randint(3, 100000)
	MY_HEIGHT = 0 # Leaf height will always be 0
	URL = "http://localhost:8080"


headers = {
    "Accept": "application/json, application/problem+json",
    "Content-Type": "application/json"
}

# hello POST Request to the server
def hello():
	try:
		# Data will be leaf's ID wrapped as a JSON object
		response = requests.post(f"{Options.URL}/hello", headers=headers, json={"Id": Options.MY_ID})
		status_code = response.status_code

		# Capture response in JSON format
		json_resp = response.json()
		return json_resp, status_code		
	except Exception as e:
		print(f"Endpoint /hello not reachable - {e}")
		return e, 404

# join POST Request to join the Vault
def join():
	try:
		# Data will be leaf's ID and height wrapped as a JSON object 
		response = requests.post(f"{Options.URL}/join", headers=headers, json={"id": Options.MY_ID, "height": Options.MY_HEIGHT, "vk-addr": "", "is-vk": False})
		status_code = response.status_code
		json_resp = response.json()
		return json_resp, status_code		
	except Exception as e:
		print(f"Endpoint /join not reachable - {e}")
		return e, 404


# register POST Request to join the Vault
def register():
	try:
		# Data will be leaf's ID and height wrapped as a JSON object 
		response = requests.post(f"{Options.URL}/register", headers=headers, json={"id": Options.MY_ID, "service": "FlaskService", "address":"127.0.0.1:1299", "stale":"3s"})
		status_code = response.status_code
		json_resp = response.json()
		return json_resp, status_code		
	except Exception as e:
		print(f"Endpoint /register not reachable - {e}")
		return e, 404

# HeartBeat POST Request to the server
def HbSend():
	try:
		# Data will be leaf's ID wrapped as a JSON object
		response = requests.post(f"{Options.URL}/service-heartbeat", headers=headers, json={"id": Options.MY_ID, "services":["FlaskService"]})
		status_code = response.status_code
		
		# Capture response in JSON format
		json_resp = response.json()
		return json_resp, status_code		
	except Exception as e:
		print(f"Endpoint /HB not reachable - {e}")
		return e, 404



# Initally, leaf is not connected to anyone
check_connection = Options.NOT_CONNECTED

# Leaf acting as client to connect to vault
def client_work():
	global check_connection
	
	# Keep on running until client dies
	while True:

		# Switch if connected
		match check_connection:

			# If not connected to VK
			case Options.NOT_CONNECTED:
				
				#Perform HELLO
				json_resp, success = hello()

				# If 200 OK
				if success == HTTPStatus.OK.value:
					# Check type
					if("id" in json_resp and "height" in json_resp):
						id_obtained = json_resp["id"]
						height_obtained = json_resp["height"]
						print(f"Obtained ID {id_obtained} and Height {height_obtained} from VK after HELLO")

						# Current consideration: If leaf's height is one less than VK's height, it can connect to that VK
						if(height_obtained == Options.MY_HEIGHT + 1):
							# Perform JOIN 
							json_resp, success = join()
							# If 200 OK
							if success == HTTPStatus.ACCEPTED.value:
								if("id" in json_resp and "height" in json_resp):									
									if json_resp["id"] != Options.MY_ID and json_resp["height"] == Options.MY_HEIGHT + 1:
										print(f"Obtained ID {json_resp['id']} and Height {json_resp['height']} from VK after JOIN")
										# Perform REGISTER 
										json_resp, success = register()
										
										# If 200 OK
										if success == HTTPStatus.ACCEPTED.value:
											if("id" in json_resp and "service" in json_resp and json_resp["id"] != Options.MY_ID):
												print(f"Service {json_resp['service']} has been Registered.")
												check_connection = Options.CONNECTED
											else:
												t = type(json_resp)
												print(f"Something went wrong during Registration. Type of data received - {t} and ID received - {json_resp['id']}")
												continue
										else:
											print("Something went wrong during Registration. Did not register to the VK")
											continue
									else:
										print(f"ID of VK I am trying to connect - {json_resp['id']}")
										print(f"Height of VK I am trying to connect - {json_resp['height']}")
										continue
								else:
									t = type(json_resp)
									print(f"Something went wrong during Join. Type of data received - {t}")
									continue

						else:
							print(f"VK: {id_obtained} is not the right parent as its height is {height_obtained}. Trying to find some other Vault Keeper...")
							continue
					else:
						t = type(json_resp)
						print(f"Something went wrong during Hello. Type of data received - {t}")
						continue
				else:
					print(json_resp)
					continue

			# If already connected to VK
			case CONNECTED:
				time.sleep(2)
				# Perform HeartBeat 
				json_resp, success = HbSend()
				print(f"HB {success}")
				# If 200 OK
				if success == HTTPStatus.OK.value:
					if("id" in json_resp and "services" in json_resp and json_resp["id"] != Options.MY_ID):
						print(f"HB response received service(s) - {json_resp['services']}")
						continue
					else:
						t = type(json_resp)
						print(f"Something went wrong during Join. Type of data received - {t} and ID received - {json_resp['id']}")
						check_connection = Options.NOT_CONNECTED
						continue
				else:
					print("Something went wrong while sending HB.")
					check_connection = Options.NOT_CONNECTED
					continue


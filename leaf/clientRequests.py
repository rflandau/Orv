import sys
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
	MY_ID = random.randInt(1, 1000000)
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
		response = requests.post(f"{Options.URL}/hello", headers=headers, json={"id": Options.MY_ID})
		status_code = response.status_code

		# Capture response in JSON format
		json_resp = response.json
		return resp_obj, status_code		
	except Exception as e:
		print(f"Endpoint /hello not reachable - {e}")
		return e, 404

# join POST Request to join the Vault
def join():
	try:
		# Data will be leaf's ID and height wrapped as a JSON object 
		response = requests.post(f"{Options.URL}/join", headers=headers, json={"id": Options.MY_ID, "height": Options.MY_HEIGHT})
		status_code = response.status_code
		json_resp = response.json
		return resp_obj, status_code		
	except Exception as e:
		print(f"Endpoint /join not reachable - {e}")
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
					if(type(json_resp) == dict):
						id_obtained = json_resp["id"]
						height_obtained = json_resp["height"]

						# Current consideration: If leaf's height is one less than VK's height, it can connect to that VK
						if(height_obtained == Options.MY_HEIGHT + 1):
							
							# Perform JOIN 
							json_resp, success = join()
							
							# If 200 OK
							if success == HTTPStatus.OK.value:
								if(type(json_resp) == dict):
									# JOINED, hence CONNECTED. 
									# TODO consider this after Registeration
									check_connection = Options.CONNECTED
									print(json_resp)
								else:
									t = type(json_resp)
									print(f"Something went wrong during Join. Type received - {t}")

						else:
							print(f"VK: {id_obtained} is not the right parent as its height is {height_obtained}. Trying to find some other Vault Keeper...")
					else:
						t = type(json_resp)
						print(f"Something went wrong during Hello. Type received - {t}")
				else:
					print(json_resp)

			# If already connected to VK
			case CONNECTED:
				print("Still working")
				break

